package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

const twitchUsersURL = "https://api.twitch.tv/helix/users"

type twitchUser struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

func (h *Handler) fetchTwitchUser(ctx context.Context, code string) (twitchUser, error) {
	ctx, cancel := context.WithTimeout(ctx, twitchTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, h.client)

	token, err := h.twitch.Exchange(ctx, code)
	if err != nil {
		return twitchUser{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, twitchUsersURL, nil)
	if err != nil {
		return twitchUser{}, err
	}
	req.Header.Set("Client-Id", h.twitch.ClientID)
	token.SetAuthHeader(req)

	res, err := h.client.Do(req)
	if err != nil {
		return twitchUser{}, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return twitchUser{}, fmt.Errorf("twitch users: %s: %s", res.Status, body)
	}

	var payload struct {
		Data []twitchUser `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return twitchUser{}, fmt.Errorf("twitch users: %w", err)
	}
	if len(payload.Data) == 0 || payload.Data[0].ID == "" || payload.Data[0].Login == "" {
		return twitchUser{}, errors.New("twitch users: empty response")
	}
	return payload.Data[0], nil
}
