package web

import (
	"net/url"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/sounds"
)

var approvalNotices = map[string]string{
	sounds.ModeratePublish: "Sound published.",
	sounds.ModerateUnlist:  "Sound unlisted.",
	sounds.ModerateDelete:  "Sound deleted.",
}

func (h *Handler) pendingQuery(c fiber.Ctx) sounds.Query {
	return sounds.Query{Sort: sounds.SortOldest, Page: pageNumber(c), Status: sounds.Visibility(sounds.VisibilityPending)}
}

func (h *Handler) approvalPage(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	if !v.Moderator() {
		return fiber.NewError(fiber.StatusForbidden, "Only moderators can see this page.")
	}

	q := h.pendingQuery(c)
	items, more, err := h.opts.Sounds.Find(c.Context(), h.baseURL(c), q)
	if err != nil {
		return err
	}
	return h.render(c, fiber.StatusOK, "approval", fiber.Map{
		"Sounds": items,
		"Pager":  newPager("/system/approval", url.Values{}, q.Page, more),
	})
}

func (h *Handler) approvalJSON(c fiber.Ctx) error {
	if _, err := h.opts.Accounts.RequireModerator(c); err != nil {
		return err
	}
	items, _, err := h.opts.Sounds.Find(c.Context(), h.baseURL(c), h.pendingQuery(c))
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (h *Handler) moderate(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	if !v.Moderator() {
		return fiber.NewError(fiber.StatusForbidden, "Only moderators can do this.")
	}

	action := c.FormValue("action")
	err := h.opts.Sounds.Moderate(c.Context(), c.FormValue("id"), action)
	if _, message, ok := userError(err); ok {
		return c.Redirect().With("error", message).To("/system/approval")
	}
	if err != nil {
		return err
	}
	return c.Redirect().With("success", approvalNotices[action]).To("/system/approval")
}

func (h *Handler) moderateJSON(c fiber.Ctx) error {
	if _, err := h.opts.Accounts.RequireModerator(c); err != nil {
		return err
	}
	if err := h.opts.Sounds.Moderate(c.Context(), c.FormValue("id"), c.FormValue("action")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
