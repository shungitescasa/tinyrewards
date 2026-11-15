package sounds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	storedMime      = "audio/ogg"
	storedExtension = "ogg"
	maxNameLength   = 255
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

func uploadName(filename string) string {
	name := strings.ToValidUTF8(filepath.Base(filename), "")
	if name == "." || name == "/" {
		return ""
	}
	if utf8.RuneCountInString(name) > maxNameLength {
		name = string([]rune(name)[:maxNameLength])
	}
	return name
}

func (h *Handler) store(ctx context.Context, uploadID, name string, file storedFile, place func() error) error {
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
	if _, err := tx.ExecContext(ctx, `INSERT INTO uploads (id, name) VALUES (?, ?)`, uploadID, name); err != nil {
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
