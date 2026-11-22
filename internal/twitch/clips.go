package twitch

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	clipsCacheTTL  = 10 * time.Minute
	clipsPageSize  = "100"
	maxLoginLength = 25
)

var (
	ErrInvalidChannel  = errors.New("channel name is not valid")
	ErrChannelNotFound = errors.New("channel not found")
	ErrNoClips         = errors.New("channel has no clips")
)

type Clip struct {
	ID              string  `json:"id"`
	URL             string  `json:"url"`
	EmbedURL        string  `json:"embed_url"`
	Title           string  `json:"title"`
	BroadcasterName string  `json:"broadcaster_name"`
	Duration        float64 `json:"duration"`
}

type clipsEntry struct {
	clips   []Clip
	expires time.Time
}

func ParseLogin(input string) (string, error) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", nil
	}
	login := fields[0]
	if i := strings.Index(strings.ToLower(login), "twitch.tv/"); i >= 0 {
		login = login[i+len("twitch.tv/"):]
		login, _, _ = strings.Cut(login, "/")
		login, _, _ = strings.Cut(login, "?")
	}
	login = strings.ToLower(strings.TrimPrefix(login, "@"))
	if login == "" || len(login) > maxLoginLength {
		return "", ErrInvalidChannel
	}
	for _, r := range login {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "", ErrInvalidChannel
		}
	}
	return login, nil
}

func (c *Client) RandomClip(ctx context.Context, userID, login string) (Clip, error) {
	broadcasterID, err := c.broadcasterID(ctx, userID, login)
	if err != nil {
		return Clip{}, err
	}
	clips, err := c.clips(ctx, userID, broadcasterID)
	if err != nil {
		return Clip{}, err
	}
	if len(clips) == 0 {
		return Clip{}, ErrNoClips
	}
	return clips[rand.IntN(len(clips))], nil
}

func (c *Client) broadcasterID(ctx context.Context, userID, login string) (string, error) {
	var res struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	err := c.call(ctx, userID, func(twitchID string) request {
		query := url.Values{"id": {twitchID}}
		if login != "" {
			query = url.Values{"login": {login}}
		}
		return request{method: http.MethodGet, path: "/users", query: query}
	}, &res)
	if isStatus(err, http.StatusBadRequest) {
		return "", ErrChannelNotFound
	}
	if err != nil {
		return "", err
	}
	if len(res.Data) == 0 {
		return "", ErrChannelNotFound
	}
	return res.Data[0].ID, nil
}

func (c *Client) clips(ctx context.Context, userID, broadcasterID string) ([]Clip, error) {
	now := time.Now()
	c.mu.Lock()
	entry, ok := c.clipCache[broadcasterID]
	c.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return entry.clips, nil
	}

	var res struct {
		Data []Clip `json:"data"`
	}
	err := c.call(ctx, userID, func(string) request {
		return request{
			method: http.MethodGet,
			path:   "/clips",
			query:  url.Values{"broadcaster_id": {broadcasterID}, "first": {clipsPageSize}},
		}
	}, &res)
	if err != nil {
		return nil, err
	}

	clips := make([]Clip, 0, len(res.Data))
	for _, clip := range res.Data {
		if clip.EmbedURL != "" {
			clips = append(clips, clip)
		}
	}

	c.mu.Lock()
	for k, v := range c.clipCache {
		if !now.Before(v.expires) {
			delete(c.clipCache, k)
		}
	}
	c.clipCache[broadcasterID] = clipsEntry{clips: clips, expires: now.Add(clipsCacheTTL)}
	c.mu.Unlock()
	return clips, nil
}
