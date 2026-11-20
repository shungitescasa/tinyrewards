package sounds

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"time"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/storage"
)

const processTimeout = 30 * time.Second

type Options struct {
	DB          *sql.DB
	Storage     storage.Storage
	MaxSize     int64
	MaxDuration time.Duration
	Deduplicate bool
	PageSize    int
	PublicURL   string

	AnonymousUploads bool
	Authenticate     fiber.Handler
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) *Handler {
	return &Handler{opts: opts}
}

func (h *Handler) Register(app, api fiber.Router) {
	api.Get("/sounds", h.List)
	api.Post("/sounds/upload", h.opts.Authenticate, h.Upload)
	app.Get(filesPath+":name", h.Download)
}

func (h *Handler) Upload(c fiber.Ctx) error {
	fh, _ := c.FormFile("sound")
	id, duration, err := h.Save(c.Context(), account.UserID(c), c.FormValue("code"), fh)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"id":       id,
		"duration": duration.Seconds(),
	})
}

func (h *Handler) Save(ctx context.Context, userID, rawCode string, fh *multipart.FileHeader) (string, time.Duration, error) {
	if userID == "" && !h.opts.AnonymousUploads {
		return "", 0, fiber.NewError(fiber.StatusUnauthorized, "authentication is required to upload sounds")
	}

	code, err := uploadCode(rawCode)
	if err != nil {
		return "", 0, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	if fh == nil {
		return "", 0, fiber.NewError(fiber.StatusBadRequest, `missing "sound" file field`)
	}
	if fh.Size == 0 {
		return "", 0, fiber.NewError(fiber.StatusBadRequest, "sound file is empty")
	}
	if fh.Size > h.opts.MaxSize {
		return "", 0, fiber.NewError(fiber.StatusRequestEntityTooLarge, fmt.Sprintf("sound file exceeds %d bytes", h.opts.MaxSize))
	}

	id, duration, err := h.ingest(ctx, userID, code, fh)
	var fe *fiber.Error
	if err != nil && !errors.As(err, &fe) {
		log.Printf("sounds: saving upload: %v", err)
		return "", 0, fiber.ErrInternalServerError
	}
	return id, duration, err
}

func (h *Handler) ingest(ctx context.Context, userID, code string, fh *multipart.FileHeader) (string, time.Duration, error) {
	src, err := fh.Open()
	if err != nil {
		return "", 0, err
	}
	defer src.Close()

	tmp, err := os.CreateTemp("", "tinyrewards-upload-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, src)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, err
	}

	out, err := os.CreateTemp("", "tinyrewards-sound-*."+storedExtension)
	if err != nil {
		return "", 0, err
	}
	processed := out.Name()
	out.Close()
	defer os.Remove(processed)

	id := rand.Text()

	ctx, cancel := context.WithTimeout(ctx, processTimeout)
	defer cancel()

	duration, err := Process(ctx, tmp.Name(), processed, h.opts.MaxDuration)
	switch {
	case errors.Is(err, ErrUnsupportedFormat):
		return "", 0, fiber.NewError(fiber.StatusUnsupportedMediaType, err.Error())
	case errors.Is(err, ErrTooLong):
		return "", 0, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrInvalidSound):
		return "", 0, fiber.NewError(fiber.StatusBadRequest, ErrInvalidSound.Error())
	case err != nil:
		return "", 0, fmt.Errorf("processing: %w", err)
	}

	salt := ""
	if !h.opts.Deduplicate {
		salt = id
	}
	size, fileID, err := digest(processed, salt)
	if err != nil {
		return "", 0, err
	}

	file := storedFile{
		ID:        fileID,
		Mime:      storedMime,
		Extension: storedExtension,
		Size:      size,
	}
	key := fileID + "." + storedExtension

	err = h.store(ctx, id, code, userID, file, func() error {
		f, err := os.Open(processed)
		if err != nil {
			return err
		}
		defer f.Close()
		return h.opts.Storage.Put(ctx, key, f, size, storedMime)
	})
	if err != nil {
		return "", 0, fmt.Errorf("storing: %w", err)
	}

	return id, duration, nil
}
