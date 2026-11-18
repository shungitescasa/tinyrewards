package widget

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/emotes"
)

//go:embed widget.html
var page []byte

type Options struct {
	DB     *sql.DB
	Emotes *emotes.Checker
}

type Handler struct {
	opts Options
}

func NewHandler(opts Options) *Handler {
	return &Handler{opts: opts}
}

func (h *Handler) Register(router fiber.Router) {
	router.Get("/widget/:user_id", h.Page)
	router.Get("/widget/:user_id/chat", h.Chat)
}

type channel struct {
	login    string
	twitchID sql.NullString
}

type chatResponse struct {
	Channel *string        `json:"channel"`
	Emotes  []emotes.Emote `json:"emotes"`
}

func (h *Handler) Page(c fiber.Ctx) error {
	if _, err := h.lookup(c, c.Params("user_id")); err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderCacheControl, "no-cache")
	return c.Send(page)
}

func (h *Handler) Chat(c fiber.Ctx) error {
	userID := c.Params("user_id")
	ch, err := h.lookup(c, userID)
	if err != nil {
		return err
	}

	res := chatResponse{Emotes: []emotes.Emote{}}
	if ch.twitchID.Valid {
		res.Channel = &ch.login
	}

	list, err := h.opts.Emotes.List(c.Context(), ch.twitchID.String)
	if err != nil {
		log.Printf("widget: loading emotes for %s: %v", userID, err)
	}
	if list != nil {
		res.Emotes = list
	}
	return c.JSON(res)
}

func (h *Handler) lookup(c fiber.Ctx, userID string) (channel, error) {
	ch, err := h.find(c.Context(), userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ch, fiber.NewError(fiber.StatusNotFound, fmt.Sprintf("user %q not found", userID))
	}
	if err != nil {
		log.Printf("widget: looking up user %s: %v", userID, err)
		return ch, fiber.ErrInternalServerError
	}
	return ch, nil
}

func (h *Handler) find(ctx context.Context, userID string) (channel, error) {
	var ch channel
	err := h.opts.DB.QueryRowContext(ctx,
		`SELECT u.username, c.alias_id
		 FROM users u
		 LEFT JOIN connections c ON c.user_id = u.id AND c.platform = ?
		 WHERE u.id = ?`,
		account.PlatformTwitch, userID,
	).Scan(&ch.login, &ch.twitchID)
	return ch, err
}
