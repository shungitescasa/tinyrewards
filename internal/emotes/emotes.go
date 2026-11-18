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
	cacheTTL        = 5 * time.Minute
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

type Emote struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Name     string `json:"name"`
}

type fetcher func(context.Context, string) ([]Emote, error)

type cacheKey struct {
	provider     string
	twitchUserID string
}

type cacheEntry struct {
	emotes  []Emote
	expires time.Time
}

type Checker struct {
	client         *http.Client
	twitchClientID string
	twitchTokens   oauth2.TokenSource

	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

func NewChecker(creds *TwitchCredentials) *Checker {
	c := &Checker{
		client: &http.Client{Timeout: requestTimeout},
		cache:  map[cacheKey]cacheEntry{},
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

func (c *Checker) fetchers(provider string) (global, channel fetcher, err error) {
	switch provider {
	case ProviderSevenTV:
		return c.sevenTVGlobal, c.sevenTVChannel, nil
	case ProviderBTTV:
		return c.bttvGlobal, c.bttvChannel, nil
	case ProviderFFZ:
		return c.ffzGlobal, c.ffzChannel, nil
	case ProviderTwitch:
		if c.twitchTokens == nil {
			return nil, nil, ErrProviderDisabled
		}
		return c.twitchGlobal, c.twitchChannel, nil
	}
	return nil, nil, ErrUnknownProvider
}

func (c *Checker) Exists(ctx context.Context, provider, twitchUserID, emoteID string) (bool, error) {
	global, channel, err := c.fetchers(provider)
	if err != nil {
		return false, err
	}

	if twitchUserID != "" {
		list, err := c.cached(ctx, provider, twitchUserID, channel)
		if err != nil {
			return false, fmt.Errorf("%s channel emotes: %w", provider, err)
		}
		if contains(list, emoteID) {
			return true, nil
		}
	}

	list, err := c.cached(ctx, provider, "", global)
	if err != nil {
		return false, fmt.Errorf("%s global emotes: %w", provider, err)
	}
	return contains(list, emoteID), nil
}

var precedence = []string{ProviderTwitch, ProviderSevenTV, ProviderBTTV, ProviderFFZ}

func (c *Checker) List(ctx context.Context, twitchUserID string) ([]Emote, error) {
	var channelEmotes, globalEmotes []Emote
	var errs []error
	for _, provider := range precedence {
		global, channel, err := c.fetchers(provider)
		if errors.Is(err, ErrProviderDisabled) {
			continue
		}
		if err != nil {
			return nil, err
		}

		if twitchUserID != "" {
			list, err := c.cached(ctx, provider, twitchUserID, channel)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s channel emotes: %w", provider, err))
			}
			channelEmotes = append(channelEmotes, list...)
		}

		list, err := c.cached(ctx, provider, "", global)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s global emotes: %w", provider, err))
		}
		globalEmotes = append(globalEmotes, list...)
	}
	return append(channelEmotes, globalEmotes...), errors.Join(errs...)
}

func (c *Checker) cached(ctx context.Context, provider, twitchUserID string, fetch fetcher) ([]Emote, error) {
	key := cacheKey{provider: provider, twitchUserID: twitchUserID}

	c.mu.Lock()
	entry, ok := c.cache[key]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.emotes, nil
	}

	list, err := fetch(ctx, twitchUserID)
	if errors.Is(err, errNotFound) {
		list, err = nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	c.mu.Lock()
	for k, v := range c.cache {
		if !now.Before(v.expires) {
			delete(c.cache, k)
		}
	}
	c.cache[key] = cacheEntry{emotes: list, expires: now.Add(cacheTTL)}
	c.mu.Unlock()
	return list, nil
}

func contains(list []Emote, id string) bool {
	for _, e := range list {
		if e.ID == id {
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

type emoteObject struct {
	ID   emoteID `json:"id"`
	Name string  `json:"name"`
	Code string  `json:"code"`
}

func collect(provider string, lists ...[]emoteObject) []Emote {
	var out []Emote
	for _, list := range lists {
		for _, e := range list {
			name := e.Name
			if name == "" {
				name = e.Code
			}
			out = append(out, Emote{Provider: provider, ID: string(e.ID), Name: name})
		}
	}
	return out
}

func (c *Checker) sevenTVGlobal(ctx context.Context, _ string) ([]Emote, error) {
	var res struct {
		Emotes []emoteObject `json:"emotes"`
	}
	if err := c.get(ctx, "https://7tv.io/v3/emote-sets/global", nil, &res); err != nil {
		return nil, err
	}
	return collect(ProviderSevenTV, res.Emotes), nil
}

func (c *Checker) sevenTVChannel(ctx context.Context, twitchUserID string) ([]Emote, error) {
	var res struct {
		EmoteSet *struct {
			Emotes []emoteObject `json:"emotes"`
		} `json:"emote_set"`
	}
	if err := c.get(ctx, "https://7tv.io/v3/users/twitch/"+url.PathEscape(twitchUserID), nil, &res); err != nil {
		return nil, err
	}
	if res.EmoteSet == nil {
		return nil, nil
	}
	return collect(ProviderSevenTV, res.EmoteSet.Emotes), nil
}

func (c *Checker) bttvGlobal(ctx context.Context, _ string) ([]Emote, error) {
	var res []emoteObject
	if err := c.get(ctx, "https://api.betterttv.net/3/cached/emotes/global", nil, &res); err != nil {
		return nil, err
	}
	return collect(ProviderBTTV, res), nil
}

func (c *Checker) bttvChannel(ctx context.Context, twitchUserID string) ([]Emote, error) {
	var res struct {
		ChannelEmotes []emoteObject `json:"channelEmotes"`
		SharedEmotes  []emoteObject `json:"sharedEmotes"`
	}
	if err := c.get(ctx, "https://api.betterttv.net/3/cached/users/twitch/"+url.PathEscape(twitchUserID), nil, &res); err != nil {
		return nil, err
	}
	return collect(ProviderBTTV, res.ChannelEmotes, res.SharedEmotes), nil
}

type ffzSet struct {
	Emoticons []emoteObject `json:"emoticons"`
}

func (c *Checker) ffzGlobal(ctx context.Context, _ string) ([]Emote, error) {
	var res struct {
		DefaultSets []emoteID         `json:"default_sets"`
		Sets        map[string]ffzSet `json:"sets"`
	}
	if err := c.get(ctx, "https://api.frankerfacez.com/v1/set/global", nil, &res); err != nil {
		return nil, err
	}
	var list []Emote
	for _, setID := range res.DefaultSets {
		list = append(list, collect(ProviderFFZ, res.Sets[string(setID)].Emoticons)...)
	}
	return list, nil
}

func (c *Checker) ffzChannel(ctx context.Context, twitchUserID string) ([]Emote, error) {
	var res struct {
		Sets map[string]ffzSet `json:"sets"`
	}
	if err := c.get(ctx, "https://api.frankerfacez.com/v1/room/id/"+url.PathEscape(twitchUserID), nil, &res); err != nil {
		return nil, err
	}
	var list []Emote
	for _, set := range res.Sets {
		list = append(list, collect(ProviderFFZ, set.Emoticons)...)
	}
	return list, nil
}

type helixEmotes struct {
	Data []emoteObject `json:"data"`
}

func (c *Checker) twitchGlobal(ctx context.Context, _ string) ([]Emote, error) {
	return c.helix(ctx, "https://api.twitch.tv/helix/chat/emotes/global")
}

func (c *Checker) twitchChannel(ctx context.Context, twitchUserID string) ([]Emote, error) {
	return c.helix(ctx, "https://api.twitch.tv/helix/chat/emotes?broadcaster_id="+url.QueryEscape(twitchUserID))
}

func (c *Checker) helix(ctx context.Context, endpoint string) ([]Emote, error) {
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
	return collect(ProviderTwitch, res.Data), nil
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
