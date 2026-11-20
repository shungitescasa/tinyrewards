package rewards

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/sounds"
)

type Options struct {
	DB           *sql.DB
	Authenticate fiber.Handler
	Emotes       *emotes.Checker
	PublicURL    string
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) *Handler {
	return &Handler{opts: opts}
}

func (h *Handler) Register(api fiber.Router) {
	api.Post("/rewards/add", h.opts.Authenticate, h.Add)
	api.Get("/rewards/:user_id", h.List)
	api.Delete("/rewards/:id", h.opts.Authenticate, h.Delete)
}

func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.Remove(c.Context(), c.Params("id"), account.UserID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) Add(c fiber.Ctx) error {
	r, err := h.Create(c.Context(), sounds.BaseURL(c, h.opts.PublicURL), account.UserID(c),
		c.FormValue("type"), c.FormValue("data"), c.FormValue("upload_id"))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(r)
}

func (h *Handler) List(c fiber.Ctx) error {
	userID := c.Params("user_id")

	items, err := h.ForUser(c.Context(), sounds.BaseURL(c, h.opts.PublicURL), userID)
	if err != nil {
		return err
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

func (h *Handler) Create(ctx context.Context, baseURL, userID, rawType, rawData, rawUploadID string) (Reward, error) {
	rewardType, err := parseType(rawType)
	if err != nil {
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	data, err := parseData(rewardType, rawData)
	if err != nil {
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	uploadID := strings.TrimSpace(rawUploadID)
	if uploadID == "" {
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, `missing "upload_id" field`)
	}

	if rewardType == TypeChatEmote {
		err := h.verifyEmote(ctx, userID, data)
		switch {
		case errors.Is(err, ErrEmoteNotFound):
			return Reward{}, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, emotes.ErrProviderDisabled):
			return Reward{}, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		case err != nil:
			log.Printf("rewards: verifying emote %s: %v", data, err)
			return Reward{}, fiber.ErrBadGateway
		}
	}

	id := rand.Text()
	err = h.insert(ctx, id, userID, rewardType, data, uploadID)
	switch {
	case errors.Is(err, ErrUploadNotFound):
		return Reward{}, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrDuplicate):
		return Reward{}, fiber.NewError(fiber.StatusConflict, err.Error())
	case isMySQLError(err, mysqlNoReferencedRow):
		return Reward{}, fiber.NewError(fiber.StatusUnauthorized, "user no longer exists")
	case err != nil:
		log.Printf("rewards: adding reward: %v", err)
		return Reward{}, fiber.ErrInternalServerError
	}

	items, err := h.find(ctx, baseURL, columnID, id)
	if err != nil || len(items) == 0 {
		log.Printf("rewards: reading added reward %s: %v", id, err)
		return Reward{}, fiber.ErrInternalServerError
	}
	return items[0], nil
}

func (h *Handler) ForUser(ctx context.Context, baseURL, userID string) ([]Reward, error) {
	items, err := h.find(ctx, baseURL, columnUserID, userID)
	if err != nil {
		log.Printf("rewards: listing rewards: %v", err)
		return nil, fiber.ErrInternalServerError
	}
	return items, nil
}

func (h *Handler) Remove(ctx context.Context, id, userID string) error {
	err := h.delete(ctx, id, userID)
	switch {
	case errors.Is(err, ErrRewardNotFound):
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	case err != nil:
		log.Printf("rewards: deleting reward: %v", err)
		return fiber.ErrInternalServerError
	}
	return nil
}
