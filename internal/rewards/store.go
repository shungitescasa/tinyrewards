package rewards

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"

	"git.shungites.casa/tiny/rewards/internal/sounds"
)

const TypeChat = "chat"

var types = []string{TypeChat}

const (
	maxDataLength        = 65535
	mysqlNoReferencedRow = 1452
	columnID             = "r.id"
	columnUserID         = "r.user_id"
)

var (
	ErrMissingType    = errors.New(`missing "type" field`)
	ErrInvalidType    = fmt.Errorf("type must be one of: %s", strings.Join(types, ", "))
	ErrMissingData    = errors.New(`missing "data" field`)
	ErrInvalidData    = errors.New("data is not valid UTF-8")
	ErrDataTooLong    = fmt.Errorf("data exceeds %d bytes", maxDataLength)
	ErrUploadNotFound = errors.New("upload not found")
)

type Reward struct {
	ID        string        `json:"id"`
	UserID    string        `json:"user_id"`
	Type      string        `json:"type"`
	Data      string        `json:"data"`
	Upload    sounds.Upload `json:"upload"`
	CreatedAt time.Time     `json:"created_at"`
}

func parseType(raw string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", ErrMissingType
	}
	for _, known := range types {
		if t == known {
			return t, nil
		}
	}
	return "", ErrInvalidType
}

func parseData(raw string) (string, error) {
	data := strings.TrimSpace(raw)
	switch {
	case data == "":
		return "", ErrMissingData
	case !utf8.ValidString(data):
		return "", ErrInvalidData
	case len(data) > maxDataLength:
		return "", ErrDataTooLong
	}
	return data, nil
}

func (h *Handler) insert(ctx context.Context, id, userID, rewardType, data, uploadID string) error {
	res, err := h.opts.DB.ExecContext(ctx,
		`INSERT INTO rewards (id, user_id, type, data, upload_id)
		 SELECT ?, ?, ?, ?, id FROM uploads WHERE id = ?`,
		id, userID, rewardType, data, uploadID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrUploadNotFound
	}
	return nil
}

func (h *Handler) find(ctx context.Context, column, value string) ([]Reward, error) {
	items := []Reward{}

	rows, err := h.opts.DB.QueryContext(ctx,
		`SELECT r.id, r.user_id, r.type, r.data, r.created_at,
		        u.id, u.code, u.user_id, u.uploaded_at,
		        f.id, f.mime, f.extension, f.size
		 FROM rewards r
		 JOIN uploads u ON u.id = r.upload_id
		 LEFT JOIN upload_files uf ON uf.upload_id = u.id
		 LEFT JOIN files f ON f.id = uf.file_id
		 WHERE `+column+` = ?
		 ORDER BY r.created_at, r.id, f.id`,
		value,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			r         Reward
			fileID    *string
			mime      *string
			extension *string
			size      *int64
		)
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Type, &r.Data, &r.CreatedAt,
			&r.Upload.ID, &r.Upload.Code, &r.Upload.UserID, &r.Upload.UploadedAt,
			&fileID, &mime, &extension, &size,
		); err != nil {
			return nil, err
		}
		if n := len(items); n == 0 || items[n-1].ID != r.ID {
			r.Upload.Files = []sounds.File{}
			items = append(items, r)
		}
		if fileID != nil {
			last := &items[len(items)-1]
			last.Upload.Files = append(last.Upload.Files, sounds.File{
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

func (h *Handler) userExists(ctx context.Context, userID string) (bool, error) {
	var exists bool
	err := h.opts.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, userID).Scan(&exists)
	return exists, err
}

func isForeignKeyViolation(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == mysqlNoReferencedRow
}
