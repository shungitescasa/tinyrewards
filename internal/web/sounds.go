package web

import (
	"net/url"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/sounds"
)

func (h *Handler) index(c fiber.Ctx) error {
	return h.render(c, fiber.StatusOK, "index", fiber.Map{"MaxDuration": h.opts.MaxDuration})
}

func (h *Handler) soundList(c fiber.Ctx) error {
	sort := sounds.SortRecent
	if c.Query("sort") == sounds.SortOldest {
		sort = sounds.SortOldest
	}
	page, search := pageNumber(c), c.Query("q")

	query := url.Values{"sort": {sort}}
	if search != "" {
		query.Set("q", search)
	}
	m := fiber.Map{"Sort": sort, "Search": search}

	items, more, err := h.opts.Sounds.Find(c.Context(), h.baseURL(c), sounds.Query{Sort: sort, Page: page, Code: search})
	status, message, ok := userError(err)
	switch {
	case ok:
		m["Error"] = message
		return h.render(c, status, "sounds", m)
	case err != nil:
		return err
	}
	m["Sounds"] = items
	m["Pager"] = newPager("/sounds", query, page, more)
	return h.render(c, fiber.StatusOK, "sounds", m)
}

func (h *Handler) uploadPage(c fiber.Ctx) error {
	return h.renderUpload(c, fiber.StatusOK, "", "", "")
}

func (h *Handler) upload(c fiber.Ctx) error {
	var userID string
	if v := h.viewer(c); v != nil {
		userID = v.ID
	}
	code, visibility := c.FormValue("code"), c.FormValue("visibility")
	fh, _ := c.FormFile("sound")

	saved, err := h.opts.Sounds.Save(c.Context(), userID, code, visibility, fh)
	if status, message, ok := userError(err); ok {
		return h.renderUpload(c, status, code, visibility, message)
	}
	if err != nil {
		return err
	}

	notice := "Sound uploaded."
	if saved.Visibility == sounds.VisibilityPending {
		notice = "Sound uploaded. It will appear in the public list once a moderator approves it."
	}
	if userID == "" {
		return c.Redirect().With("success", notice).To("/sounds")
	}
	return c.Redirect().With("success", notice+" Now choose what triggers it.").
		To("/rewards/new?upload=" + url.QueryEscape(saved.ID))
}

func (h *Handler) renderUpload(c fiber.Ctx, status int, code, visibility, message string) error {
	return h.render(c, status, "upload", fiber.Map{
		"Allowed":     h.opts.AnonymousUploads || h.viewer(c) != nil,
		"Code":        code,
		"Unlisted":    visibility == "unlisted",
		"Moderation":  h.opts.Sounds.ModerationEnabled(),
		"Error":       message,
		"MaxSize":     formatSize(h.opts.MaxSize),
		"MaxDuration": h.opts.MaxDuration,
	})
}

func (h *Handler) deleteSound(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	err := h.opts.Sounds.Remove(c.Context(), c.FormValue("id"), v.ID)
	if _, message, ok := userError(err); ok {
		return c.Redirect().With("error", message).Back("/sounds")
	}
	if err != nil {
		return err
	}
	return c.Redirect().With("success", "Sound deleted.").Back("/sounds")
}
