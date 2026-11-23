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
	if header == "" {
		return h.authenticateSession(c, required)
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
	return nil
}

func (h *Handler) authenticateSession(c fiber.Ctx, required bool) error {
	token := c.Cookies(SessionCookie)
	if token == "" {
		if required {
			return fiber.NewError(fiber.StatusUnauthorized, "missing bearer token")
		}
		return nil
	}
	userID, err := h.tokens.verify(token, time.Now())
	if err != nil {
		if required {
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		}
		return nil
	}
	c.Locals(userIDKey{}, userID)
	return nil
}

func UserID(c fiber.Ctx) string {
	id, _ := c.Locals(userIDKey{}).(string)
	return id
}

func (h *Handler) Me(c fiber.Ctx) error {
	if err := h.Authenticate(c); err != nil {
		return err
	}
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

func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.Authenticate(c); err != nil {
		return err
	}
	if err := h.Remove(c.Context(), UserID(c)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fiber.NewError(fiber.StatusUnauthorized, "user no longer exists")
		}
		log.Printf("account: delete: %v", err)
		return fiber.ErrInternalServerError
	}
	h.ClearSession(c)
	return c.SendStatus(fiber.StatusNoContent)
}
