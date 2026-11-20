package account

import (
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

type userIDKey struct{}

func (h *Handler) Authenticate(c fiber.Ctx) error {
	return h.authenticate(c, true)
}

func (h *Handler) OptionalAuthenticate(c fiber.Ctx) error {
	return h.authenticate(c, false)
}

func (h *Handler) authenticate(c fiber.Ctx, required bool) error {
	header := c.Get(fiber.HeaderAuthorization)
	if header == "" && !required {
		return c.Next()
	}
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
	}
	userID, err := h.tokens.verify(strings.TrimSpace(token), time.Now())
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, err.Error())
	}
	c.Locals(userIDKey{}, userID)
	return c.Next()
}

func UserID(c fiber.Ctx) string {
	id, _ := c.Locals(userIDKey{}).(string)
	return id
}

func (h *Handler) Me(c fiber.Ctx) error {
	u, err := h.FindByID(c.Context(), UserID(c))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.NewError(fiber.StatusUnauthorized, "user no longer exists")
		}
		log.Printf("account: me: %v", err)
		return fiber.ErrInternalServerError
	}
	return c.JSON(u)
}
