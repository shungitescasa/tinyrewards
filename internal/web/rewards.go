package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/rewards"
	"git.shungites.casa/tiny/rewards/internal/sounds"
	"git.shungites.casa/tiny/rewards/internal/twitch"
)

const recentUploads = 5

func (h *Handler) rewardList(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	ctx, baseURL := c.Context(), h.baseURL(c)

	items, err := h.opts.Rewards.ForUser(ctx, baseURL, v.ID)
	if err != nil {
		return err
	}

	every := c.Query("emote_mode") == "every"
	delay, _ := strconv.ParseUint(c.Query("delay"), 10, 32)
	query := url.Values{}
	if every {
		query.Set("emote_mode", "every")
	}
	if delay > 0 {
		query.Set("delay", strconv.FormatUint(delay, 10))
	}
	widget := baseURL + "/widget/" + url.PathEscape(v.ID)
	if len(query) > 0 {
		widget += "?" + query.Encode()
	}

	return h.render(c, fiber.StatusOK, "rewards", fiber.Map{
		"Rewards":      items,
		"EmoteNames":   h.emoteNames(ctx, h.twitchID(ctx, v.ID), items),
		"PointRewards": h.pointRewards(ctx, v.ID, items),
		"Owner":        true,
		"WidgetURL":    widget,
		"Every":        every,
		"Delay":        delay,
	})
}

func (h *Handler) rewardPage(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	return h.renderReward(c, fiber.StatusOK, v, c.Query("type"), c.Query("action"), c.Query("upload"), "", "")
}

func (h *Handler) createReward(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	ctx := c.Context()
	rewardType, action, uploadID, input := c.FormValue("type"), c.FormValue("action"), c.FormValue("upload_id"), c.FormValue("data")

	data, err := input, error(nil)
	if rewardType == rewards.TypeChatEmote {
		data, err = h.resolveEmote(ctx, h.twitchID(ctx, v.ID), input)
	}
	if err == nil {
		_, err = h.opts.Rewards.Create(ctx, h.baseURL(c), v.ID, rewardType, data, action, uploadID)
	}
	if status, message, ok := userError(err); ok {
		return h.renderReward(c, status, v, rewardType, action, uploadID, input, message)
	}
	if err != nil {
		return err
	}
	return c.Redirect().With("success", "Reward created.").To("/rewards")
}

func (h *Handler) deleteReward(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	err := h.opts.Rewards.Remove(c.Context(), c.Params("id"), v.ID)
	if _, message, ok := userError(err); ok {
		return c.Redirect().With("error", message).To("/rewards")
	}
	if err != nil {
		return err
	}
	return c.Redirect().With("success", "Reward deleted.").To("/rewards")
}

func (h *Handler) renderReward(c fiber.Ctx, status int, v *account.User, rewardType, action, uploadID, data, message string) error {
	ctx, baseURL := c.Context(), h.baseURL(c)
	anonupload, points := h.opts.Rewards.AnonuploadEnabled(), h.opts.Rewards.ChannelPointsEnabled()
	switch {
	case action == rewards.ActionAnonupload && anonupload:
	case action == rewards.ActionTwitchClip && points, action == rewards.ActionYouTube && points:
	case action == rewards.ActionSkip:
	default:
		action = rewards.ActionSound
	}
	switch {
	case action != rewards.ActionSound && action != rewards.ActionSkip:
		rewardType = rewards.TypeTwitchReward
	case rewardType == rewards.TypeChatEmote, rewardType == rewards.TypeTwitchReward:
	default:
		rewardType = rewards.TypeChat
	}
	m := fiber.Map{"Type": rewardType, "Action": action, "Anonupload": anonupload, "ChannelPoints": points, "Data": data, "Error": message}

	var upload *sounds.Upload
	if action == rewards.ActionSound && uploadID != "" {
		u, err := h.opts.Sounds.Get(ctx, baseURL, uploadID)
		_, msg, ok := userError(err)
		switch {
		case ok:
			m["Error"] = msg
		case err != nil:
			return err
		default:
			upload = &u
		}
	}
	m["Upload"] = upload

	if action == rewards.ActionSound && upload == nil {
		uploads, _, err := h.opts.Sounds.Find(ctx, baseURL, sounds.Query{Sort: sounds.SortRecent, Page: 1, Limit: recentUploads, UserID: v.ID, All: true})
		if err != nil {
			return err
		}
		m["Uploads"] = uploads
	}

	if rewardType == rewards.TypeChatEmote {
		emoteFilter(m, h.channelEmotes(ctx, h.twitchID(ctx, v.ID)), c.Query("provider"), c.Query("q"))
	}

	if rewardType == rewards.TypeTwitchReward {
		h.channelPointRewards(ctx, v.ID, m)
	}

	return h.render(c, status, "reward", m)
}

func emoteFilter(m fiber.Map, list []emotes.Emote, provider, query string) {
	present := map[string]bool{}
	for _, e := range list {
		present[e.Provider] = true
	}
	var providers []string
	for _, p := range emotes.Providers {
		if present[p] {
			providers = append(providers, p)
		}
	}
	if !present[provider] {
		provider = ""
	}
	query = strings.TrimSpace(query)
	needle := strings.ToLower(query)

	var shown []emotes.Emote
	for _, e := range list {
		if (provider == "" || e.Provider == provider) && strings.Contains(strings.ToLower(e.Name), needle) {
			shown = append(shown, e)
		}
	}
	m["Emotes"] = shown
	m["EmoteTotal"] = len(list)
	m["EmoteProviders"] = providers
	m["EmoteProvider"] = provider
	m["EmoteQuery"] = query
}

func (h *Handler) channelPointRewards(ctx context.Context, userID string, m fiber.Map) {
	m["TwitchEnabled"] = h.opts.Twitch != nil
	if h.opts.Twitch == nil {
		return
	}
	list, err := h.opts.Twitch.Rewards(ctx, userID)
	switch {
	case errors.Is(err, twitch.ErrNotAuthorized):
		m["PointRewardsLogin"] = true
	case errors.Is(err, twitch.ErrNotAffiliate):
		m["PointRewardsError"] = "Channel points are only available to Twitch partners and affiliates."
	case err != nil:
		log.Printf("web: loading channel point rewards: %v", err)
		m["PointRewardsError"] = "Could not load your channel point rewards from Twitch, try again later."
	}
	m["PointRewards"] = list
}

func (h *Handler) pointRewards(ctx context.Context, userID string, items []rewards.Reward) map[string]*twitch.Reward {
	found := map[string]*twitch.Reward{}
	if h.opts.Twitch == nil || !slices.ContainsFunc(items, func(r rewards.Reward) bool { return r.Type == rewards.TypeTwitchReward }) {
		return found
	}
	list, err := h.opts.Twitch.CachedRewards(ctx, userID)
	if err != nil && !errors.Is(err, twitch.ErrNotAuthorized) && !errors.Is(err, twitch.ErrNotAffiliate) {
		log.Printf("web: loading channel point rewards: %v", err)
	}
	for i := range list {
		found[list[i].ID] = &list[i]
	}
	return found
}

func (h *Handler) resolveEmote(ctx context.Context, twitchID, input string) (string, error) {
	name := strings.TrimSpace(input)
	if name == "" {
		return "", nil
	}
	for _, e := range h.channelEmotes(ctx, twitchID) {
		if e.Provider+":"+e.ID == name || e.Name == name {
			return e.Provider + ":" + e.ID, nil
		}
	}
	if provider, _, ok := strings.Cut(name, ":"); ok && h.opts.Emotes.Enabled(strings.ToLower(provider)) {
		return name, nil
	}
	return "", fiber.NewError(fiber.StatusUnprocessableEntity, fmt.Sprintf("emote %q is not available in your channel or globally", name))
}

func (h *Handler) emoteNames(ctx context.Context, twitchID string, items []rewards.Reward) map[string]string {
	names := map[string]string{}
	if !slices.ContainsFunc(items, func(r rewards.Reward) bool { return r.Type == rewards.TypeChatEmote }) {
		return names
	}
	for _, e := range h.channelEmotes(ctx, twitchID) {
		key := e.Provider + ":" + e.ID
		if _, ok := names[key]; !ok {
			names[key] = e.Name
		}
	}
	return names
}
