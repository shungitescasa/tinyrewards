package sounds

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v3"
)

const processTimeout = 30 * time.Second

type Options struct {
	DB          *sql.DB
	Directory   string
	MaxSize     int64
	MaxDuration time.Duration
	Deduplicate bool
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) (*Handler, error) {
	if err := os.MkdirAll(opts.Directory, 0o755); err != nil {
		return nil, err
	}
	return &Handler{opts: opts}, nil
}

func (h *Handler) Register(router fiber.Router) {
	router.Post("/sounds/upload", h.Upload)
}

func (h *Handler) Upload(c fiber.Ctx) error {
	fh, err := c.FormFile("sound")
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, `missing "sound" file field`)
	}
	if fh.Size == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "sound file is empty")
	}
	if fh.Size > h.opts.MaxSize {
		return fiber.NewError(fiber.StatusRequestEntityTooLarge, fmt.Sprintf("sound file exceeds %d bytes", h.opts.MaxSize))
	}

	src, err := fh.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "tinyrewards-upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}

	id := rand.Text()
	partial := filepath.Join(h.opts.Directory, "."+id+".part")

	ctx, cancel := context.WithTimeout(c.Context(), processTimeout)
	defer cancel()

	duration, err := Process(ctx, tmp.Name(), partial, h.opts.MaxDuration)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnsupportedFormat):
			return fiber.NewError(fiber.StatusUnsupportedMediaType, err.Error())
		case errors.Is(err, ErrTooLong):
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrInvalidSound):
			return fiber.NewError(fiber.StatusBadRequest, ErrInvalidSound.Error())
		default:
			log.Printf("sounds: processing upload: %v", err)
			return fiber.ErrInternalServerError
		}
	}

	salt := ""
	if !h.opts.Deduplicate {
		salt = id
	}
	size, fileID, err := digest(partial, salt)
	if err != nil {
		os.Remove(partial)
		return err
	}

	file := storedFile{
		ID:        fileID,
		Mime:      storedMime,
		Extension: storedExtension,
		Size:      size,
	}
	final := filepath.Join(h.opts.Directory, fileID+"."+storedExtension)

	err = h.store(ctx, id, uploadName(fh.Filename), file, func() error {
		return os.Rename(partial, final)
	})
	if err != nil {
		os.Remove(partial)
		log.Printf("sounds: storing upload: %v", err)
		return fiber.ErrInternalServerError
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":       id,
		"duration": duration.Seconds(),
	})
}
