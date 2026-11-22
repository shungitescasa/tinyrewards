package twitch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/fasthttp/websocket"
)

const (
	eventSubURL      = "wss://eventsub.wss.twitch.tv/ws"
	redemptionType   = "channel.channel_points_custom_reward_redemption.add"
	welcomeTimeout   = 10 * time.Second
	minKeepalive     = 10 * time.Second
	keepaliveMargin  = 5 * time.Second
	idleTimeout      = 30 * time.Second
	maxBackoff       = time.Minute
	maxBackoffShift  = 6
	maxListeners     = 16
	listenerBuffer   = 16
	maxMessageSize   = 1 << 20
	recentMessageIDs = 64
)

const (
	messageConnected  = "connected"
	messageError      = "error"
	messageRedemption = "redemption"
)

var (
	ErrTooManyListeners = errors.New("too many widgets are listening to this channel")
	errConnectionLost   = errors.New("lost connection to Twitch, reconnecting")
	errKeepaliveTimeout = errors.New("no keepalive received in time")
)

type redemption struct {
	ID          string    `json:"id"`
	RewardID    string    `json:"reward_id"`
	RewardTitle string    `json:"reward_title"`
	UserLogin   string    `json:"user_login"`
	UserName    string    `json:"user_name"`
	UserInput   string    `json:"user_input"`
	RedeemedAt  time.Time `json:"redeemed_at"`
}

type message struct {
	Type       string      `json:"type"`
	Message    string      `json:"message,omitempty"`
	Redemption *redemption `json:"redemption,omitempty"`
}

type session struct {
	ID                      string `json:"id"`
	KeepaliveTimeoutSeconds int    `json:"keepalive_timeout_seconds"`
	ReconnectURL            string `json:"reconnect_url"`
}

func (s session) keepalive() time.Duration {
	return max(minKeepalive, time.Duration(s.KeepaliveTimeoutSeconds)*time.Second) + keepaliveMargin
}

type envelope struct {
	Metadata struct {
		MessageID        string `json:"message_id"`
		MessageType      string `json:"message_type"`
		SubscriptionType string `json:"subscription_type"`
	} `json:"metadata"`
	Payload struct {
		Session      *session `json:"session"`
		Subscription *struct {
			Status string `json:"status"`
		} `json:"subscription"`
		Event json.RawMessage `json:"event"`
	} `json:"payload"`
}

type redemptionEvent struct {
	ID         string    `json:"id"`
	UserLogin  string    `json:"user_login"`
	UserName   string    `json:"user_name"`
	UserInput  string    `json:"user_input"`
	RedeemedAt time.Time `json:"redeemed_at"`
	Reward     struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"reward"`
}

type frame struct {
	conn *websocket.Conn
	data []byte
	err  error
}

type Listener struct {
	C     <-chan []byte
	close func()
}

func (l *Listener) Close() {
	l.close()
}

type channel struct {
	userID    string
	listeners map[chan []byte]struct{}
	status    []byte
	cancel    context.CancelFunc
	idle      *time.Timer
}

type EventSub struct {
	client *Client
	dialer *websocket.Dialer

	mu       sync.Mutex
	channels map[string]*channel
}

func NewEventSub(client *Client) *EventSub {
	return &EventSub{
		client:   client,
		dialer:   &websocket.Dialer{HandshakeTimeout: requestTimeout},
		channels: map[string]*channel{},
	}
}

func (e *EventSub) Listen(userID string) (*Listener, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	ch := e.channels[userID]
	if ch == nil {
		ctx, cancel := context.WithCancel(context.Background())
		ch = &channel{userID: userID, listeners: map[chan []byte]struct{}{}, cancel: cancel}
		e.channels[userID] = ch
		go e.run(ctx, ch)
	}
	if len(ch.listeners) >= maxListeners {
		return nil, ErrTooManyListeners
	}
	if ch.idle != nil {
		ch.idle.Stop()
		ch.idle = nil
	}

	c := make(chan []byte, listenerBuffer)
	if ch.status != nil {
		c <- ch.status
	}
	ch.listeners[c] = struct{}{}
	return &Listener{C: c, close: sync.OnceFunc(func() { e.leave(ch, c) })}, nil
}

func (e *EventSub) leave(ch *channel, c chan []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()

	delete(ch.listeners, c)
	if len(ch.listeners) > 0 || ch.idle != nil {
		return
	}
	var timer *time.Timer
	timer = time.AfterFunc(idleTimeout, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if ch.idle != timer || e.channels[ch.userID] != ch {
			return
		}
		ch.cancel()
		delete(e.channels, ch.userID)
	})
	ch.idle = timer
}

func (e *EventSub) broadcast(ch *channel, m message, status bool) {
	data, err := json.Marshal(m)
	if err != nil {
		log.Printf("twitch: encoding eventsub message: %v", err)
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if status {
		ch.status = data
	}
	for c := range ch.listeners {
		select {
		case c <- data:
		default:
		}
	}
}

func (e *EventSub) run(ctx context.Context, ch *channel) {
	for attempt := 0; ; attempt++ {
		err := e.session(ctx, ch, func() { attempt = 0 })
		if ctx.Err() != nil {
			return
		}

		wait, public := min(maxBackoff, time.Second<<min(attempt, maxBackoffShift)), errConnectionLost
		if errors.Is(err, ErrNotAuthorized) || errors.Is(err, ErrNotAffiliate) {
			wait, public = maxBackoff, err
		} else {
			log.Printf("twitch: eventsub for user %s: %v, reconnecting in %s", ch.userID, err, wait)
		}
		e.broadcast(ch, message{Type: messageError, Message: public.Error()}, true)

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

func (e *EventSub) session(ctx context.Context, ch *channel, connected func()) error {
	if _, err := e.client.token(ctx, ch.userID, ""); err != nil {
		return err
	}

	active, welcome, err := e.connect(ctx)
	if err != nil {
		return err
	}
	var pending *websocket.Conn
	defer func() {
		active.Close()
		if pending != nil {
			pending.Close()
		}
	}()

	if err := e.client.subscribe(ctx, ch.userID, welcome.ID); err != nil {
		return err
	}
	connected()
	e.broadcast(ch, message{Type: messageConnected}, true)

	frames := make(chan frame)
	done := make(chan struct{})
	defer close(done)
	read := func(conn *websocket.Conn) {
		go func() {
			for {
				_, data, err := conn.ReadMessage()
				select {
				case frames <- frame{conn: conn, data: data, err: err}:
				case <-done:
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}
	read(active)

	timeout := welcome.keepalive()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var recent [recentMessageIDs]string
	next := 0

	for {
		var f frame
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return errKeepaliveTimeout
		case f = <-frames:
		}

		if f.err != nil {
			switch f.conn {
			case active:
				return f.err
			case pending:
				pending.Close()
				pending = nil
			}
			continue
		}

		var env envelope
		if err := json.Unmarshal(f.data, &env); err != nil {
			return fmt.Errorf("decoding eventsub message: %w", err)
		}
		if f.conn == active {
			timer.Reset(timeout)
		}

		switch env.Metadata.MessageType {
		case "session_welcome":
			if f.conn != pending || env.Payload.Session == nil {
				continue
			}
			active.Close()
			active, pending = pending, nil
			timeout = env.Payload.Session.keepalive()
			timer.Reset(timeout)

		case "session_reconnect":
			if pending != nil || env.Payload.Session == nil || env.Payload.Session.ReconnectURL == "" {
				continue
			}
			conn, err := e.dial(ctx, env.Payload.Session.ReconnectURL)
			if err != nil {
				return err
			}
			pending = conn
			read(pending)

		case "notification":
			id := env.Metadata.MessageID
			if id != "" {
				if slices.Contains(recent[:], id) {
					continue
				}
				recent[next] = id
				next = (next + 1) % recentMessageIDs
			}
			e.notify(ch, env)

		case "revocation":
			status := ""
			if env.Payload.Subscription != nil {
				status = env.Payload.Subscription.Status
			}
			if status == "authorization_revoked" || status == "user_removed" {
				return ErrNotAuthorized
			}
			return fmt.Errorf("subscription revoked: %s", status)
		}
	}
}

func (e *EventSub) notify(ch *channel, env envelope) {
	if env.Metadata.SubscriptionType != redemptionType {
		return
	}
	var ev redemptionEvent
	if err := json.Unmarshal(env.Payload.Event, &ev); err != nil {
		log.Printf("twitch: decoding redemption for user %s: %v", ch.userID, err)
		return
	}
	e.broadcast(ch, message{Type: messageRedemption, Redemption: &redemption{
		ID:          ev.ID,
		RewardID:    ev.Reward.ID,
		RewardTitle: ev.Reward.Title,
		UserLogin:   ev.UserLogin,
		UserName:    ev.UserName,
		UserInput:   ev.UserInput,
		RedeemedAt:  ev.RedeemedAt,
	}}, false)
}

func (e *EventSub) connect(ctx context.Context) (*websocket.Conn, session, error) {
	conn, err := e.dial(ctx, eventSubURL)
	if err != nil {
		return nil, session{}, err
	}

	var env envelope
	err = conn.SetReadDeadline(time.Now().Add(welcomeTimeout))
	if err == nil {
		err = conn.ReadJSON(&env)
	}
	if err == nil && (env.Metadata.MessageType != "session_welcome" || env.Payload.Session == nil) {
		err = fmt.Errorf("expected session_welcome, got %q", env.Metadata.MessageType)
	}
	if err == nil {
		err = conn.SetReadDeadline(time.Time{})
	}
	if err != nil {
		conn.Close()
		return nil, session{}, fmt.Errorf("reading eventsub welcome: %w", err)
	}
	return conn, *env.Payload.Session, nil
}

func (e *EventSub) dial(ctx context.Context, endpoint string) (*websocket.Conn, error) {
	conn, _, err := e.dialer.DialContext(ctx, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("connecting to eventsub: %w", err)
	}
	conn.SetReadLimit(maxMessageSize)
	return conn, nil
}

func (c *Client) subscribe(ctx context.Context, userID, sessionID string) error {
	err := c.call(ctx, userID, func(twitchID string) request {
		return request{
			method: http.MethodPost,
			path:   "/eventsub/subscriptions",
			body: map[string]any{
				"type":      redemptionType,
				"version":   "1",
				"condition": map[string]string{"broadcaster_user_id": twitchID},
				"transport": map[string]string{"method": "websocket", "session_id": sessionID},
			},
		}
	}, nil)
	if isStatus(err, http.StatusForbidden) {
		return ErrNotAuthorized
	}
	if err != nil {
		return fmt.Errorf("subscribing to redemptions: %w", err)
	}
	return nil
}
