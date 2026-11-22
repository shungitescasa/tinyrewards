package twitch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const tokenLeeway = time.Minute

type userToken struct {
	twitchID string
	access   string
	refresh  string
	scopes   []string
	expires  time.Time
}

func (t userToken) usable(stale string) bool {
	return t.access != stale && (t.expires.IsZero() || time.Now().Add(tokenLeeway).Before(t.expires))
}

func (c *Client) SaveToken(ctx context.Context, userID, twitchID string, token *oauth2.Token) error {
	err := c.store(ctx, userID, userToken{
		twitchID: twitchID,
		access:   token.AccessToken,
		refresh:  token.RefreshToken,
		scopes:   tokenScopes(token),
		expires:  token.Expiry,
	})
	if err != nil {
		return err
	}
	c.forget(userID)
	return nil
}

func (c *Client) token(ctx context.Context, userID, stale string) (userToken, error) {
	t, err := c.load(ctx, userID)
	if err != nil || t.usable(stale) {
		return t, err
	}

	c.refreshing.Lock()
	defer c.refreshing.Unlock()

	t, err = c.load(ctx, userID)
	if err != nil || t.usable(stale) {
		return t, err
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()

	token, err := c.oauth.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, c.http), &oauth2.Token{RefreshToken: t.refresh}).Token()
	var re *oauth2.RetrieveError
	if errors.As(err, &re) && re.Response != nil && re.Response.StatusCode >= http.StatusBadRequest && re.Response.StatusCode < http.StatusInternalServerError {
		if _, err := c.db.ExecContext(ctx, `DELETE FROM twitch_tokens WHERE user_id = ?`, userID); err != nil {
			return t, err
		}
		c.forget(userID)
		return t, ErrNotAuthorized
	}
	if err != nil {
		return t, fmt.Errorf("refreshing twitch token: %w", err)
	}

	t.access, t.refresh, t.expires = token.AccessToken, token.RefreshToken, token.Expiry
	if scopes := tokenScopes(token); len(scopes) > 0 {
		t.scopes = scopes
	}
	if err := c.store(ctx, userID, t); err != nil {
		return t, err
	}
	if !slices.Contains(t.scopes, ScopeReadRedemptions) {
		return t, ErrNotAuthorized
	}
	return t, nil
}

func (c *Client) load(ctx context.Context, userID string) (userToken, error) {
	var (
		t       userToken
		scopes  string
		expires sql.NullTime
	)
	err := c.db.QueryRowContext(ctx,
		`SELECT twitch_id, access_token, refresh_token, scopes, expires_at FROM twitch_tokens WHERE user_id = ?`,
		userID,
	).Scan(&t.twitchID, &t.access, &t.refresh, &scopes, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotAuthorized
	}
	if err != nil {
		return t, err
	}
	t.scopes = strings.Fields(scopes)
	t.expires = expires.Time
	if !slices.Contains(t.scopes, ScopeReadRedemptions) {
		return t, ErrNotAuthorized
	}
	return t, nil
}

func (c *Client) store(ctx context.Context, userID string, t userToken) error {
	var expires sql.NullTime
	if !t.expires.IsZero() {
		expires = sql.NullTime{Time: t.expires.UTC(), Valid: true}
	}
	_, err := c.db.ExecContext(ctx,
		`REPLACE INTO twitch_tokens (user_id, twitch_id, access_token, refresh_token, scopes, expires_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, t.twitchID, t.access, t.refresh, strings.Join(t.scopes, " "), expires,
	)
	return err
}

func tokenScopes(token *oauth2.Token) []string {
	switch v := token.Extra("scope").(type) {
	case []any:
		scopes := make([]string, 0, len(v))
		for _, s := range v {
			if s, ok := s.(string); ok {
				scopes = append(scopes, s)
			}
		}
		return scopes
	case string:
		return strings.Fields(v)
	}
	return nil
}
