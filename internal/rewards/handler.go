package rewards

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/emotes"
)

type Options struct {
	DB           *sql.DB
	Authenticate fiber.Handler
	Emotes       *emotes.Checker
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) *Handler {
	return &Handler{opts: opts}
}

func (h *Handler) Register(router fiber.Router) {
	router.Post("/rewards/add", h.opts.Authenticate, h.Add)
	router.Get("/rewards/:user_id", h.List)
}

func (h *Handler) Add(c fiber.Ctx) error {
	userID := account.UserID(c)

	rewardType, err := parseType(c.FormValue("type"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	data, err := parseData(rewardType, c.FormValue("data"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	uploadID := strings.TrimSpace(c.FormValue("upload_id"))
	if uploadID == "" {
		return fiber.NewError(fiber.StatusBadRequest, `missing "upload_id" field`)
	}

	if rewardType == TypeChatEmote {
		err := h.verifyEmote(c.Context(), userID, data)
		switch {
		case errors.Is(err, ErrEmoteNotFound):
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, emotes.ErrProviderDisabled):
			return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		case err != nil:
			log.Printf("rewards: verifying emote %s: %v", data, err)
			return fiber.ErrBadGateway
		}
	}

	id := rand.Text()
	err = h.insert(c.Context(), id, userID, rewardType, data, uploadID)
	switch {
	case errors.Is(err, ErrUploadNotFound):
		return fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrDuplicate):
		return fiber.NewError(fiber.StatusConflict, err.Error())
	case isMySQLError(err, mysqlNoReferencedRow):
		return fiber.NewError(fiber.StatusUnauthorized, "user no longer exists")
	case err != nil:
		log.Printf("rewards: adding reward: %v", err)
		return fiber.ErrInternalServerError
	}

	items, err := h.find(c.Context(), columnID, id)
	if err != nil || len(items) == 0 {
		log.Printf("rewards: reading added reward %s: %v", id, err)
		return fiber.ErrInternalServerError
	}

	return c.Status(fiber.StatusCreated).JSON(items[0])
}

func (h *Handler) List(c fiber.Ctx) error {
	userID := c.Params("user_id")

	items, err := h.find(c.Context(), columnUserID, userID)
	if err != nil {
		log.Printf("rewards: listing rewards: %v", err)
		return fiber.ErrInternalServerError
	}

	if len(items) == 0 {
		exists, err := h.userExists(c.Context(), userID)
		if err != nil {
			log.Printf("rewards: checking user: %v", err)
			return fiber.ErrInternalServerError
		}
		if !exists {
			return fiber.NewError(fiber.StatusNotFound, fmt.Sprintf("user %q not found", userID))
		}
	}

	return c.JSON(items)
}
