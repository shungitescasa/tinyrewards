package rewards

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"log"
	"strings"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/anonupload"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/sounds"
	"git.shungites.casa/tiny/rewards/internal/twitch"
)

type Options struct {
	DB           *sql.DB
	Authenticate fiber.Handler
	Emotes       *emotes.Checker
	Twitch       *twitch.Client
	EventSub     *twitch.EventSub
	Anonupload   *anonupload.Client
	PublicURL    string
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) *Handler {
	return &Handler{opts: opts}
}

func (h *Handler) Register(app fiber.Router) {
	app.Get("/rewards/events", h.eventsUpgrade, websocket.New(h.Events))
}

func (h *Handler) AnonuploadEnabled() bool {
	return h.opts.Anonupload != nil
}

func (h *Handler) ChannelPointsEnabled() bool {
	return h.opts.Twitch != nil
}

func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.opts.Authenticate(c); err != nil {
		return err
	}
	if err := h.Remove(c.Context(), c.Params("id"), account.UserID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) Add(c fiber.Ctx) error {
	if err := h.opts.Authenticate(c); err != nil {
		return err
	}
	r, err := h.Create(c.Context(), sounds.BaseURL(c, h.opts.PublicURL), account.UserID(c),
		c.FormValue("type"), c.FormValue("data"), c.FormValue("action"), c.FormValue("upload_id"))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(r)
}

func (h *Handler) Mine(c fiber.Ctx) error {
	if err := h.opts.Authenticate(c); err != nil {
		return err
	}
	items, err := h.ForUser(c.Context(), sounds.BaseURL(c, h.opts.PublicURL), account.UserID(c))
	if err != nil {
		return err
	}
	return c.JSON(items)
}

func (h *Handler) Create(ctx context.Context, baseURL, userID, rawType, rawData, rawAction, rawUploadID string) (Reward, error) {
	rewardType, err := parseType(rawType)
	if err != nil {
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	data, err := parseData(rewardType, rawData)
	if err != nil {
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	action, err := parseAction(rawAction)
	if err != nil {
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	uploadID := strings.TrimSpace(rawUploadID)
	switch {
	case action == ActionSound && uploadID == "":
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, `missing "upload_id" field`)
	case action == ActionAnonupload && !h.AnonuploadEnabled():
		return Reward{}, fiber.NewError(fiber.StatusUnprocessableEntity, ErrAnonDisabled.Error())
	case (action == ActionTwitchClip || action == ActionYouTube) && !h.ChannelPointsEnabled():
		return Reward{}, fiber.NewError(fiber.StatusUnprocessableEntity, ErrTwitchDisabled.Error())
	case action != ActionSound && rewardType != TypeTwitchReward:
		return Reward{}, fiber.NewError(fiber.StatusBadRequest, ErrActionType.Error())
	}

	switch rewardType {
	case TypeChatEmote:
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
	case TypeTwitchReward:
		err := h.verifyTwitchReward(ctx, userID, data)
		switch {
		case errors.Is(err, ErrTwitchNotFound), errors.Is(err, ErrTwitchDisabled),
			errors.Is(err, twitch.ErrNotAuthorized), errors.Is(err, twitch.ErrNotAffiliate):
			return Reward{}, fiber.NewError(fiber.StatusUnprocessableEntity, err.Error())
		case err != nil:
			log.Printf("rewards: verifying channel point reward %s: %v", data, err)
			return Reward{}, fiber.ErrBadGateway
		}
	}

	id := rand.Text()
	err = h.insert(ctx, id, userID, rewardType, data, action, uploadID)
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
