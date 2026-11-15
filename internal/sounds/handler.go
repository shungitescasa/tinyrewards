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
	"time"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/storage"
)

const processTimeout = 30 * time.Second

type Options struct {
	DB          *sql.DB
	Storage     storage.Storage
	MaxSize     int64
	MaxDuration time.Duration
	Deduplicate bool
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) *Handler {
	return &Handler{opts: opts}
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

	out, err := os.CreateTemp("", "tinyrewards-sound-*."+storedExtension)
	if err != nil {
		return err
	}
	processed := out.Name()
	out.Close()
	defer os.Remove(processed)

	id := rand.Text()

	ctx, cancel := context.WithTimeout(c.Context(), processTimeout)
	defer cancel()

	duration, err := Process(ctx, tmp.Name(), processed, h.opts.MaxDuration)
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
	size, fileID, err := digest(processed, salt)
	if err != nil {
		return err
	}

	file := storedFile{
		ID:        fileID,
		Mime:      storedMime,
		Extension: storedExtension,
		Size:      size,
	}
	key := fileID + "." + storedExtension

	err = h.store(ctx, id, uploadName(fh.Filename), file, func() error {
		f, err := os.Open(processed)
		if err != nil {
			return err
		}
		defer f.Close()
		return h.opts.Storage.Put(ctx, key, f, size, storedMime)
	})
	if err != nil {
		log.Printf("sounds: storing upload: %v", err)
		return fiber.ErrInternalServerError
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":       id,
		"duration": duration.Seconds(),
	})
}
