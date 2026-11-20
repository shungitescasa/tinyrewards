package web

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/rewards"
	"git.shungites.casa/tiny/rewards/internal/sounds"
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
		"Rewards":    items,
		"EmoteNames": h.emoteNames(ctx, h.twitchID(ctx, v.ID), items),
		"Owner":      true,
		"WidgetURL":  widget,
		"Every":      every,
		"Delay":      delay,
	})
}

func (h *Handler) rewardPage(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	return h.renderReward(c, fiber.StatusOK, v, c.Query("type"), c.Query("upload"), "", "")
}

func (h *Handler) createReward(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	ctx := c.Context()
	rewardType, uploadID, input := c.FormValue("type"), c.FormValue("upload_id"), c.FormValue("data")

	data, err := input, error(nil)
	if rewardType == rewards.TypeChatEmote {
		data, err = h.resolveEmote(ctx, h.twitchID(ctx, v.ID), input)
	}
	if err == nil {
		_, err = h.opts.Rewards.Create(ctx, h.baseURL(c), v.ID, rewardType, data, uploadID)
	}
	if status, message, ok := userError(err); ok {
		return h.renderReward(c, status, v, rewardType, uploadID, input, message)
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

func (h *Handler) renderReward(c fiber.Ctx, status int, v *account.User, rewardType, uploadID, data, message string) error {
	ctx, baseURL := c.Context(), h.baseURL(c)
	if rewardType != rewards.TypeChatEmote {
		rewardType = rewards.TypeChat
	}
	m := fiber.Map{"Type": rewardType, "Data": data, "Error": message}

	var upload *sounds.Upload
	if uploadID != "" {
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

	if upload == nil {
		uploads, _, err := h.opts.Sounds.Find(ctx, baseURL, sounds.Query{Sort: sounds.SortRecent, Page: 1, Limit: recentUploads, UserID: v.ID})
		if err != nil {
			return err
		}
		m["Uploads"] = uploads
	}

	if rewardType == rewards.TypeChatEmote {
		var options []emotes.Emote
		seen := map[string]bool{}
		for _, e := range h.channelEmotes(ctx, h.twitchID(ctx, v.ID)) {
			if !seen[e.Name] {
				seen[e.Name] = true
				options = append(options, e)
			}
		}
		m["Emotes"] = options
	}

	return h.render(c, status, "reward", m)
}

func (h *Handler) resolveEmote(ctx context.Context, twitchID, input string) (string, error) {
	name := strings.TrimSpace(input)
	if name == "" {
		return "", nil
	}
	for _, e := range h.channelEmotes(ctx, twitchID) {
		if e.Name == name {
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
