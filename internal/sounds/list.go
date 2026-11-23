package sounds

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v3"
)

const (
	SortRecent = "recent"
	SortOldest = "oldest"

	maxCodePattern = 200
)

type File struct {
	ID        string `json:"id"`
	Mime      string `json:"mime"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
	URL       string `json:"url"`
}

type Upload struct {
	ID         string    `json:"id"`
	Code       string    `json:"code"`
	UserID     *string   `json:"user_id"`
	Username   *string   `json:"username"`
	UploadedAt time.Time `json:"uploaded_at"`
	Files      []File    `json:"files"`
}

type Query struct {
	Sort   string
	Page   int64
	Limit  int64
	UserID string
	Code   string
}

func (h *Handler) List(c fiber.Ctx) error {
	q := Query{Sort: c.Query("sort", SortRecent), Page: 1, Code: c.Query("q")}
	if raw := c.Query("p"); raw != "" {
		p, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "p must be a positive integer")
		}
		q.Page = p
	}

	items, _, err := h.Find(c.Context(), BaseURL(c, h.opts.PublicURL), q)
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (h *Handler) Find(ctx context.Context, baseURL string, q Query) ([]Upload, bool, error) {
	var direction string
	switch q.Sort {
	case SortRecent:
		direction = "DESC"
	case SortOldest:
		direction = "ASC"
	default:
		return nil, false, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf(`sort must be %q or %q`, SortRecent, SortOldest))
	}

	pageSize := int64(h.opts.PageSize)
	if q.Limit > 0 {
		pageSize = q.Limit
	}
	if q.Page < 1 || q.Page-1 > math.MaxInt64/pageSize {
		return nil, false, fiber.NewError(fiber.StatusBadRequest, "p must be a positive integer")
	}

	if len(q.Code) > maxCodePattern {
		return nil, false, fiber.NewError(fiber.StatusBadRequest, fmt.Sprintf("search pattern must be at most %d characters", maxCodePattern))
	}

	filter, args := "TRUE", []any{}
	if q.UserID != "" {
		filter, args = filter+" AND user_id = ?", append(args, q.UserID)
	}
	if q.Code != "" {
		filter, args = filter+" AND code REGEXP ?", append(args, q.Code)
	}

	items, err := h.query(ctx, baseURL, filter, args, direction, pageSize+1, (q.Page-1)*pageSize)
	if message, ok := regexpError(err); ok {
		return nil, false, fiber.NewError(fiber.StatusBadRequest, "invalid search pattern: "+message)
	}
	if err != nil {
		log.Printf("sounds: listing uploads: %v", err)
		return nil, false, fiber.ErrInternalServerError
	}

	more := int64(len(items)) > pageSize
	if more {
		items = items[:pageSize]
	}
	return items, more, nil
}

func regexpError(err error) (string, bool) {
	var me *mysql.MySQLError
	if !errors.As(err, &me) || (me.Number != 1139 && (me.Number < 3684 || me.Number > 3699)) {
		return "", false
	}
	return me.Message, true
}

func (h *Handler) Get(ctx context.Context, baseURL, id string) (Upload, error) {
	items, err := h.query(ctx, baseURL, "id = ?", []any{id}, "DESC", 1, 0)
	if err != nil {
		log.Printf("sounds: reading upload %s: %v", id, err)
		return Upload{}, fiber.ErrInternalServerError
	}
	if len(items) == 0 {
		return Upload{}, fiber.NewError(fiber.StatusNotFound, "upload not found")
	}
	return items[0], nil
}

func (h *Handler) query(ctx context.Context, baseURL, filter string, args []any, direction string, limit, offset int64) ([]Upload, error) {
	items := []Upload{}

	query := fmt.Sprintf(
		`SELECT u.id, u.code, u.user_id, usr.username, u.uploaded_at, f.id, f.mime, f.extension, f.size
		 FROM (
		     SELECT id, code, user_id, uploaded_at FROM uploads
		     WHERE %[2]s
		     ORDER BY uploaded_at %[1]s, id %[1]s
		     LIMIT ? OFFSET ?
		 ) u
		 LEFT JOIN users usr ON usr.id = u.user_id
		 LEFT JOIN upload_files uf ON uf.upload_id = u.id
		 LEFT JOIN files f ON f.id = uf.file_id
		 ORDER BY u.uploaded_at %[1]s, u.id %[1]s, f.id`,
		direction, filter,
	)
	rows, err := h.opts.DB.QueryContext(ctx, query, append(args, limit, offset)...)
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
		if err := rows.Scan(&u.ID, &u.Code, &u.UserID, &u.Username, &u.UploadedAt, &fileID, &mime, &extension, &size); err != nil {
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
				URL:       FileURL(baseURL, *fileID, *extension),
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
