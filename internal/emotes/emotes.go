package emotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
	"golang.org/x/oauth2/twitch"
)

const (
	ProviderSevenTV = "7tv"
	ProviderBTTV    = "bttv"
	ProviderFFZ     = "ffz"
	ProviderTwitch  = "twitch"
)

var Providers = []string{ProviderSevenTV, ProviderBTTV, ProviderFFZ, ProviderTwitch}

const (
	requestTimeout  = 15 * time.Second
	maxResponseSize = 16 << 20
	channelCacheTTL = 5 * time.Minute
)

var (
	ErrUnknownProvider  = errors.New("unknown emote provider")
	ErrProviderDisabled = errors.New("emote provider is not configured")
	errNotFound         = errors.New("not found")
)

type TwitchCredentials struct {
	ClientID     string
	ClientSecret string
}

type channelKey struct {
	provider     string
	twitchUserID string
}

type channelEntry struct {
	ids     []string
	expires time.Time
}

type Checker struct {
	client         *http.Client
	twitchClientID string
	twitchTokens   oauth2.TokenSource

	mu       sync.Mutex
	channels map[channelKey]channelEntry
}

func NewChecker(creds *TwitchCredentials) *Checker {
	c := &Checker{
		client:   &http.Client{Timeout: requestTimeout},
		channels: map[channelKey]channelEntry{},
	}
	if creds != nil {
		cfg := clientcredentials.Config{
			ClientID:     creds.ClientID,
			ClientSecret: creds.ClientSecret,
			TokenURL:     twitch.Endpoint.TokenURL,
			AuthStyle:    oauth2.AuthStyleInParams,
		}
		ctx := context.WithValue(context.Background(), oauth2.HTTPClient, c.client)
		c.twitchClientID = creds.ClientID
		c.twitchTokens = cfg.TokenSource(ctx)
	}
	return c
}

func (c *Checker) Exists(ctx context.Context, provider, twitchUserID, emoteID string) (bool, error) {
	var global, channel func(context.Context, string) ([]string, error)
	switch provider {
	case ProviderSevenTV:
		global, channel = c.sevenTVGlobal, c.sevenTVChannel
	case ProviderBTTV:
		global, channel = c.bttvGlobal, c.bttvChannel
	case ProviderFFZ:
		global, channel = c.ffzGlobal, c.ffzChannel
	case ProviderTwitch:
		if c.twitchTokens == nil {
			return false, ErrProviderDisabled
		}
		global, channel = c.twitchGlobal, c.twitchChannel
	default:
		return false, ErrUnknownProvider
	}

	if twitchUserID != "" {
		ids, err := c.channelEmotes(ctx, provider, twitchUserID, channel)
		if err != nil {
			return false, fmt.Errorf("%s channel emotes: %w", provider, err)
		}
		if contains(ids, emoteID) {
			return true, nil
		}
	}

	ids, err := global(ctx, "")
	if err != nil {
		return false, fmt.Errorf("%s global emotes: %w", provider, err)
	}
	return contains(ids, emoteID), nil
}

func (c *Checker) channelEmotes(ctx context.Context, provider, twitchUserID string, fetch func(context.Context, string) ([]string, error)) ([]string, error) {
	key := channelKey{provider: provider, twitchUserID: twitchUserID}

	c.mu.Lock()
	entry, ok := c.channels[key]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.ids, nil
	}

	ids, err := fetch(ctx, twitchUserID)
	if errors.Is(err, errNotFound) {
		ids, err = nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	c.mu.Lock()
	for k, v := range c.channels {
		if !now.Before(v.expires) {
			delete(c.channels, k)
		}
	}
	c.channels[key] = channelEntry{ids: ids, expires: now.Add(channelCacheTTL)}
	c.mu.Unlock()
	return ids, nil
}

func contains(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

type emoteID string

func (id *emoteID) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*id = emoteID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*id = emoteID(n.String())
	return nil
}

type idObject struct {
	ID emoteID `json:"id"`
}

func collect(lists ...[]idObject) []string {
	var ids []string
	for _, list := range lists {
		for _, e := range list {
			ids = append(ids, string(e.ID))
		}
	}
	return ids
}

func (c *Checker) sevenTVGlobal(ctx context.Context, _ string) ([]string, error) {
	var res struct {
		Emotes []idObject `json:"emotes"`
	}
	if err := c.get(ctx, "https://7tv.io/v3/emote-sets/global", nil, &res); err != nil {
		return nil, err
	}
	return collect(res.Emotes), nil
}

func (c *Checker) sevenTVChannel(ctx context.Context, twitchUserID string) ([]string, error) {
	var res struct {
		EmoteSet *struct {
			Emotes []idObject `json:"emotes"`
		} `json:"emote_set"`
	}
	if err := c.get(ctx, "https://7tv.io/v3/users/twitch/"+url.PathEscape(twitchUserID), nil, &res); err != nil {
		return nil, err
	}
	if res.EmoteSet == nil {
		return nil, nil
	}
	return collect(res.EmoteSet.Emotes), nil
}

func (c *Checker) bttvGlobal(ctx context.Context, _ string) ([]string, error) {
	var res []idObject
	if err := c.get(ctx, "https://api.betterttv.net/3/cached/emotes/global", nil, &res); err != nil {
		return nil, err
	}
	return collect(res), nil
}

func (c *Checker) bttvChannel(ctx context.Context, twitchUserID string) ([]string, error) {
	var res struct {
		ChannelEmotes []idObject `json:"channelEmotes"`
		SharedEmotes  []idObject `json:"sharedEmotes"`
	}
	if err := c.get(ctx, "https://api.betterttv.net/3/cached/users/twitch/"+url.PathEscape(twitchUserID), nil, &res); err != nil {
		return nil, err
	}
	return collect(res.ChannelEmotes, res.SharedEmotes), nil
}

type ffzSet struct {
	Emoticons []idObject `json:"emoticons"`
}

func (c *Checker) ffzGlobal(ctx context.Context, _ string) ([]string, error) {
	var res struct {
		DefaultSets []emoteID         `json:"default_sets"`
		Sets        map[string]ffzSet `json:"sets"`
	}
	if err := c.get(ctx, "https://api.frankerfacez.com/v1/set/global", nil, &res); err != nil {
		return nil, err
	}
	var ids []string
	for _, setID := range res.DefaultSets {
		ids = append(ids, collect(res.Sets[string(setID)].Emoticons)...)
	}
	return ids, nil
}

func (c *Checker) ffzChannel(ctx context.Context, twitchUserID string) ([]string, error) {
	var res struct {
		Sets map[string]ffzSet `json:"sets"`
	}
	if err := c.get(ctx, "https://api.frankerfacez.com/v1/room/id/"+url.PathEscape(twitchUserID), nil, &res); err != nil {
		return nil, err
	}
	var ids []string
	for _, set := range res.Sets {
		ids = append(ids, collect(set.Emoticons)...)
	}
	return ids, nil
}

type helixEmotes struct {
	Data []idObject `json:"data"`
}

func (c *Checker) twitchGlobal(ctx context.Context, _ string) ([]string, error) {
	return c.helix(ctx, "https://api.twitch.tv/helix/chat/emotes/global")
}

func (c *Checker) twitchChannel(ctx context.Context, twitchUserID string) ([]string, error) {
	return c.helix(ctx, "https://api.twitch.tv/helix/chat/emotes?broadcaster_id="+url.QueryEscape(twitchUserID))
}

func (c *Checker) helix(ctx context.Context, endpoint string) ([]string, error) {
	token, err := c.twitchTokens.Token()
	if err != nil {
		return nil, fmt.Errorf("twitch app token: %w", err)
	}
	header := http.Header{}
	header.Set("Client-Id", c.twitchClientID)
	header.Set("Authorization", "Bearer "+token.AccessToken)

	var res helixEmotes
	if err := c.get(ctx, endpoint, header, &res); err != nil {
		return nil, err
	}
	return collect(res.Data), nil
}

func (c *Checker) get(ctx context.Context, endpoint string, header http.Header, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: unexpected status %d: %s", endpoint, res.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxResponseSize)).Decode(out)
}
