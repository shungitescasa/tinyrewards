package twitch

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/twitch"
)

const ScopeReadRedemptions = "channel:read:redemptions"

const (
	helixURL        = "https://api.twitch.tv/helix"
	requestTimeout  = 15 * time.Second
	maxResponseSize = 1 << 20
	maxErrorSize    = 4 << 10
)

var (
	ErrNotAuthorized = errors.New("channel points access is not granted, log in with Twitch again")
	ErrNotAffiliate  = errors.New("channel points are only available to Twitch partners and affiliates")
)

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("twitch api: %d %s", e.status, e.message)
}

func isStatus(err error, status int) bool {
	var ae *apiError
	return errors.As(err, &ae) && ae.status == status
}

type Options struct {
	DB           *sql.DB
	ClientID     string
	ClientSecret string
}

type Client struct {
	db         *sql.DB
	clientID   string
	oauth      *oauth2.Config
	http       *http.Client
	refreshing sync.Mutex

	mu      sync.Mutex
	rewards map[string]rewardsEntry
}

func NewClient(opts Options) *Client {
	return &Client{
		db:       opts.DB,
		clientID: opts.ClientID,
		oauth: &oauth2.Config{
			ClientID:     opts.ClientID,
			ClientSecret: opts.ClientSecret,
			Endpoint:     twitch.Endpoint,
		},
		http:    &http.Client{Timeout: requestTimeout},
		rewards: map[string]rewardsEntry{},
	}
}

type request struct {
	method string
	path   string
	query  url.Values
	body   any
}

func (c *Client) call(ctx context.Context, userID string, build func(twitchID string) request, out any) error {
	tok, err := c.token(ctx, userID, "")
	if err != nil {
		return err
	}
	r := build(tok.twitchID)
	err = c.send(ctx, tok, r, out)
	if !isStatus(err, http.StatusUnauthorized) {
		return err
	}

	if tok, err = c.token(ctx, userID, tok.access); err != nil {
		return err
	}
	err = c.send(ctx, tok, r, out)
	if isStatus(err, http.StatusUnauthorized) {
		return ErrNotAuthorized
	}
	return err
}

func (c *Client) send(ctx context.Context, tok userToken, r request, out any) error {
	var body io.Reader
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}

	endpoint := helixURL + r.path
	if len(r.query) > 0 {
		endpoint += "?" + r.query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, r.method, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Client-Id", c.clientID)
	req.Header.Set("Authorization", "Bearer "+tok.access)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode > 299 {
		var payload struct {
			Message string `json:"message"`
		}
		json.NewDecoder(io.LimitReader(res.Body, maxErrorSize)).Decode(&payload)
		return &apiError{status: res.StatusCode, message: payload.Message}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxResponseSize)).Decode(out)
}
