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
	page := pageNumber(c)

	items, more, err := h.opts.Sounds.Find(c.Context(), h.baseURL(c), sounds.Query{Sort: sort, Page: page})
	if err != nil {
		return err
	}
	return h.render(c, fiber.StatusOK, "sounds", fiber.Map{
		"Sort":   sort,
		"Sounds": items,
		"Pager":  newPager("/sounds", url.Values{"sort": {sort}}, page, more),
	})
}

func (h *Handler) uploadPage(c fiber.Ctx) error {
	return h.renderUpload(c, fiber.StatusOK, "", "")
}

func (h *Handler) upload(c fiber.Ctx) error {
	var userID string
	if v := h.viewer(c); v != nil {
		userID = v.ID
	}
	code := c.FormValue("code")
	fh, _ := c.FormFile("sound")

	id, _, err := h.opts.Sounds.Save(c.Context(), userID, code, fh)
	if status, message, ok := userError(err); ok {
		return h.renderUpload(c, status, code, message)
	}
	if err != nil {
		return err
	}

	if userID == "" {
		return c.Redirect().With("success", "Sound uploaded.").To("/sounds")
	}
	return c.Redirect().With("success", "Sound uploaded. Now choose what triggers it.").
		To("/rewards/new?upload=" + url.QueryEscape(id))
}

func (h *Handler) renderUpload(c fiber.Ctx, status int, code, message string) error {
	return h.render(c, status, "upload", fiber.Map{
		"Allowed":     h.opts.AnonymousUploads || h.viewer(c) != nil,
		"Code":        code,
		"Error":       message,
		"MaxSize":     formatSize(h.opts.MaxSize),
		"MaxDuration": h.opts.MaxDuration,
	})
}
