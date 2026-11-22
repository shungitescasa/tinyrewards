package rewards

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

const (
	eventsKeepalive    = 30 * time.Second
	eventsWriteTimeout = 10 * time.Second
	eventsReadLimit    = 512
	mediaTimeout       = 15 * time.Second
)

var keepaliveMessage = []byte(`{"type":"keepalive"}`)

func (h *Handler) eventsUpgrade(c fiber.Ctx) error {
	if h.opts.EventSub == nil {
		return fiber.NewError(fiber.StatusNotImplemented, ErrTwitchDisabled.Error())
	}
	if !websocket.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}
	userID := c.Query("user_id")
	if userID == "" {
		return fiber.NewError(fiber.StatusBadRequest, `missing "user_id" query parameter`)
	}
	exists, err := h.userExists(c.Context(), userID)
	if err != nil {
		log.Printf("rewards: checking user: %v", err)
		return fiber.ErrInternalServerError
	}
	if !exists {
		return fiber.NewError(fiber.StatusNotFound, fmt.Sprintf("user %q not found", userID))
	}
	return c.Next()
}

func (h *Handler) Events(conn *websocket.Conn) {
	defer conn.Close()

	userID := conn.Query("user_id")
	listener, err := h.opts.EventSub.Listen(userID)
	if err != nil {
		conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, err.Error()),
			time.Now().Add(eventsWriteTimeout))
		return
	}
	defer listener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		defer cancel()
		conn.SetReadLimit(eventsReadLimit)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(eventsKeepalive)
	defer ticker.Stop()

	for {
		var data []byte
		select {
		case <-closed:
			return
		case data = <-listener.C:
			data = h.withMedia(ctx, userID, data)
		case <-ticker.C:
			data = keepaliveMessage
		}
		conn.SetWriteDeadline(time.Now().Add(eventsWriteTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}

func (h *Handler) withMedia(ctx context.Context, userID string, data []byte) []byte {
	if h.opts.Anonupload == nil {
		return data
	}
	var msg struct {
		Type       string `json:"type"`
		Redemption *struct {
			RewardID string `json:"reward_id"`
		} `json:"redemption"`
	}
	if err := json.Unmarshal(data, &msg); err != nil || msg.Type != "redemption" || msg.Redemption == nil {
		return data
	}

	action, err := h.actionFor(ctx, userID, TypeTwitchReward, msg.Redemption.RewardID)
	if err != nil {
		log.Printf("rewards: looking up reward action for %s: %v", userID, err)
		return data
	}
	if action != ActionAnonupload {
		return data
	}

	ctx, cancel := context.WithTimeout(ctx, mediaTimeout)
	defer cancel()
	media, err := h.opts.Anonupload.Random(ctx)
	if err != nil {
		log.Printf("rewards: fetching random anonupload post for %s: %v", userID, err)
		return data
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data
	}
	if fields["media"], err = json.Marshal(media); err != nil {
		return data
	}
	out, err := json.Marshal(fields)
	if err != nil {
		return data
	}
	return out
}
