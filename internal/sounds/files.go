package sounds

import (
	"database/sql"
	"errors"
	"io/fs"
	"log"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const filesPath = "/files/"

func BaseURL(c fiber.Ctx, publicURL string) string {
	if publicURL != "" {
		return publicURL
	}
	return c.BaseURL()
}

func FileURL(baseURL, id, extension string) string {
	return baseURL + filesPath + url.PathEscape(id+"."+extension)
}

func (h *Handler) Download(c fiber.Ctx) error {
	id, extension, ok := strings.Cut(c.Params("name"), ".")
	if !ok || id == "" || extension == "" {
		return fiber.NewError(fiber.StatusNotFound, "file not found")
	}

	var (
		mime string
		size int64
	)
	err := h.opts.DB.QueryRowContext(c.Context(),
		`SELECT mime, size FROM files WHERE id = ? AND extension = ?`,
		id, extension,
	).Scan(&mime, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "file not found")
	}
	if err != nil {
		log.Printf("sounds: looking up file %s: %v", id, err)
		return fiber.ErrInternalServerError
	}

	r, err := h.opts.Storage.Open(c.Context(), id+"."+extension)
	if errors.Is(err, fs.ErrNotExist) {
		return fiber.NewError(fiber.StatusNotFound, "file not found")
	}
	if err != nil {
		log.Printf("sounds: opening file %s: %v", id, err)
		return fiber.ErrInternalServerError
	}

	c.Set(fiber.HeaderContentType, mime)
	c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
	return c.SendStream(r, int(size))
}
