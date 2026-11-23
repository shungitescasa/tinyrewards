package sounds

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrMissingCode = errors.New(`missing "code" field`)
	ErrInvalidCode = errors.New("code contains invalid characters")
	ErrCodeTooLong = fmt.Errorf("code exceeds %d characters", maxCodeLength)
)

const (
	storedMime      = "audio/ogg"
	storedExtension = "ogg"
	maxCodeLength   = 255
)

type storedFile struct {
	ID        string
	Mime      string
	Extension string
	Size      int64
}

func digest(path, salt string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	io.WriteString(h, salt)
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func uploadCode(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	switch {
	case code == "":
		return "", ErrMissingCode
	case !utf8.ValidString(code):
		return "", ErrInvalidCode
	case utf8.RuneCountInString(code) > maxCodeLength:
		return "", ErrCodeTooLong
	case strings.IndexFunc(code, unicode.IsControl) >= 0:
		return "", ErrInvalidCode
	}
	return code, nil
}

func (h *Handler) store(ctx context.Context, uploadID, code, userID string, visibility int, file storedFile, place func() error) error {
	tx, err := h.opts.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO files (id, mime, extension, size) VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE id = id`,
		file.ID, file.Mime, file.Extension, file.Size,
	)
	if err != nil {
		return err
	}
	inserted, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO uploads (id, code, user_id, visibility) VALUES (?, ?, ?, ?)`,
		uploadID, code, sql.NullString{String: userID, Valid: userID != ""}, visibility,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO upload_files (upload_id, file_id) VALUES (?, ?)`, uploadID, file.ID); err != nil {
		return err
	}

	if inserted != 0 {
		if err := place(); err != nil {
			return err
		}
	}
	return tx.Commit()
}
