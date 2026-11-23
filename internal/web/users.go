package web

import (
	"database/sql"
	"errors"
	"net/url"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/sounds"
)

func (h *Handler) loginPage(c fiber.Ctx) error {
	if h.viewer(c) != nil {
		return c.Redirect().To("/account")
	}
	return h.render(c, fiber.StatusOK, "login", fiber.Map{"Twitch": h.opts.Accounts.TwitchEnabled()})
}

func (h *Handler) accountPage(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	conns, err := h.opts.Accounts.Connections(c.Context(), v.ID)
	if err != nil {
		return err
	}
	return h.render(c, fiber.StatusOK, "account", fiber.Map{
		"Connections": conns,
		"Token":       c.Cookies(account.SessionCookie),
	})
}

func (h *Handler) userPage(c fiber.Ctx) error {
	ctx, baseURL := c.Context(), h.baseURL(c)

	u, err := h.opts.Accounts.FindByID(ctx, c.Params("id"))
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "This user does not exist.")
	}
	if err != nil {
		return err
	}

	items, err := h.opts.Rewards.ForUser(ctx, baseURL, u.ID)
	if err != nil {
		return err
	}

	page := pageNumber(c)
	uploads, more, err := h.opts.Sounds.Find(ctx, baseURL, sounds.Query{Sort: sounds.SortRecent, Page: page, UserID: u.ID})
	if err != nil {
		return err
	}

	pages := newPager("/users/"+url.PathEscape(u.ID), url.Values{}, page, more)
	if pages.Prev != "" {
		pages.Prev += "#sounds"
	}
	if pages.Next != "" {
		pages.Next += "#sounds"
	}

	twitchID := h.twitchID(ctx, u.ID)
	v := h.viewer(c)
	return h.render(c, fiber.StatusOK, "user", fiber.Map{
		"User":         u,
		"Twitch":       twitchID != "",
		"Self":         v != nil && v.ID == u.ID,
		"Rewards":      items,
		"EmoteNames":   h.emoteNames(ctx, twitchID, items),
		"PointRewards": h.pointRewards(ctx, u.ID, items),
		"Sounds":       uploads,
		"Pager":        pages,
	})
}

func (h *Handler) userJSON(c fiber.Ctx) error {
	ctx, baseURL := c.Context(), h.baseURL(c)

	u, err := h.opts.Accounts.FindByID(ctx, c.Params("id"))
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "user not found")
	}
	if err != nil {
		return err
	}

	items, err := h.opts.Rewards.ForUser(ctx, baseURL, u.ID)
	if err != nil {
		return err
	}

	uploads, _, err := h.opts.Sounds.Find(ctx, baseURL, sounds.Query{Sort: sounds.SortRecent, Page: pageNumber(c), UserID: u.ID})
	if err != nil {
		return err
	}

	return c.JSON(fiber.Map{
		"user":    u,
		"rewards": items,
		"sounds":  uploads,
	})
}

func (h *Handler) deleteAccountPage(c fiber.Ctx) error {
	if h.viewer(c) == nil {
		return toLogin(c)
	}
	return h.render(c, fiber.StatusOK, "account_delete", nil)
}

func (h *Handler) deleteAccount(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	if err := h.opts.Accounts.Remove(c.Context(), v.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	h.opts.Accounts.ClearSession(c)
	return c.Redirect().With("success", "Your account has been deleted.").To("/")
}
