package twitch

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const rewardsCacheTTL = time.Minute

type Reward struct {
	ID      string
	Title   string
	Cost    int64
	Color   string
	Image   string
	Enabled bool
	Paused  bool
}

type rewardsEntry struct {
	rewards []Reward
	expires time.Time
}

type rewardImage struct {
	URL2x string `json:"url_2x"`
}

func (c *Client) Rewards(ctx context.Context, userID string) ([]Reward, error) {
	var res struct {
		Data []struct {
			ID              string       `json:"id"`
			Title           string       `json:"title"`
			Cost            int64        `json:"cost"`
			BackgroundColor string       `json:"background_color"`
			IsEnabled       bool         `json:"is_enabled"`
			IsPaused        bool         `json:"is_paused"`
			Image           *rewardImage `json:"image"`
			DefaultImage    *rewardImage `json:"default_image"`
		} `json:"data"`
	}
	err := c.call(ctx, userID, func(twitchID string) request {
		return request{
			method: http.MethodGet,
			path:   "/channel_points/custom_rewards",
			query:  url.Values{"broadcaster_id": {twitchID}},
		}
	}, &res)
	if isStatus(err, http.StatusForbidden) {
		return nil, ErrNotAffiliate
	}
	if err != nil {
		return nil, err
	}

	list := make([]Reward, 0, len(res.Data))
	for _, r := range res.Data {
		reward := Reward{
			ID:      r.ID,
			Title:   r.Title,
			Cost:    r.Cost,
			Color:   r.BackgroundColor,
			Enabled: r.IsEnabled,
			Paused:  r.IsPaused,
		}
		for _, img := range []*rewardImage{r.Image, r.DefaultImage} {
			if img != nil && img.URL2x != "" {
				reward.Image = img.URL2x
				break
			}
		}
		list = append(list, reward)
	}
	slices.SortStableFunc(list, func(a, b Reward) int {
		return cmp.Or(cmp.Compare(a.Cost, b.Cost), strings.Compare(a.Title, b.Title))
	})

	now := time.Now()
	c.mu.Lock()
	for k, v := range c.rewards {
		if !now.Before(v.expires) {
			delete(c.rewards, k)
		}
	}
	c.rewards[userID] = rewardsEntry{rewards: list, expires: now.Add(rewardsCacheTTL)}
	c.mu.Unlock()
	return list, nil
}

func (c *Client) CachedRewards(ctx context.Context, userID string) ([]Reward, error) {
	c.mu.Lock()
	entry, ok := c.rewards[userID]
	c.mu.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.rewards, nil
	}
	return c.Rewards(ctx, userID)
}

func (c *Client) HasReward(ctx context.Context, userID, rewardID string) (bool, error) {
	list, err := c.Rewards(ctx, userID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(list, func(r Reward) bool { return r.ID == rewardID }), nil
}

func (c *Client) forget(userID string) {
	c.mu.Lock()
	delete(c.rewards, userID)
	c.mu.Unlock()
}
