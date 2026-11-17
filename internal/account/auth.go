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
	scheme, token, ok := strings.Cut(c.Get(fiber.HeaderAuthorization), " ")
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
	u, err := h.findByID(c.Context(), UserID(c))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.NewError(fiber.StatusUnauthorized, "user no longer exists")
		}
		log.Printf("account: me: %v", err)
		return fiber.ErrInternalServerError
	}
	return c.JSON(u)
}
