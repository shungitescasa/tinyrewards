package sounds

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
)

const (
	SortRecent = "recent"
	SortOldest = "oldest"
)

type File struct {
	ID        string `json:"id"`
	Mime      string `json:"mime"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
}

type Upload struct {
	ID         string    `json:"id"`
	Code       string    `json:"code"`
	UserID     *string   `json:"user_id"`
	UploadedAt time.Time `json:"uploaded_at"`
	Files      []File    `json:"files"`
}

func (h *Handler) List(c fiber.Ctx) error {
	sort := c.Query("sort", SortRecent)
	var direction string
	switch sort {
	case SortRecent:
		direction = "DESC"
	case SortOldest:
		direction = "ASC"
	default:
		return fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf(`sort must be %q or %q`, SortRecent, SortOldest))
	}

	pageSize := int64(h.opts.PageSize)
	page := int64(1)
	if raw := c.Query("p"); raw != "" {
		p, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || p < 1 || p-1 > math.MaxInt64/pageSize {
			return fiber.NewError(fiber.StatusBadRequest, "p must be a positive integer")
		}
		page = p
	}

	items, err := h.list(c.Context(), direction, pageSize, (page-1)*pageSize)
	if err != nil {
		log.Printf("sounds: listing uploads: %v", err)
		return fiber.ErrInternalServerError
	}

	return c.JSON(items)
}

func (h *Handler) list(ctx context.Context, direction string, limit, offset int64) ([]Upload, error) {
	items := []Upload{}

	query := fmt.Sprintf(
		`SELECT u.id, u.code, u.user_id, u.uploaded_at, f.id, f.mime, f.extension, f.size
		 FROM (
		     SELECT id, code, user_id, uploaded_at FROM uploads
		     ORDER BY uploaded_at %[1]s, id %[1]s
		     LIMIT ? OFFSET ?
		 ) u
		 LEFT JOIN upload_files uf ON uf.upload_id = u.id
		 LEFT JOIN files f ON f.id = uf.file_id
		 ORDER BY u.uploaded_at %[1]s, u.id %[1]s, f.id`,
		direction,
	)
	rows, err := h.opts.DB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			u         Upload
			fileID    *string
			mime      *string
			extension *string
			size      *int64
		)
		if err := rows.Scan(&u.ID, &u.Code, &u.UserID, &u.UploadedAt, &fileID, &mime, &extension, &size); err != nil {
			return nil, err
		}
		if n := len(items); n == 0 || items[n-1].ID != u.ID {
			u.Files = []File{}
			items = append(items, u)
		}
		if fileID != nil {
			last := &items[len(items)-1]
			last.Files = append(last.Files, File{
				ID:        *fileID,
				Mime:      *mime,
				Extension: *extension,
				Size:      *size,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
