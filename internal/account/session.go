package account

import (
	"database/sql"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
)

const SessionCookie = "session"

func (h *Handler) SessionUser(c fiber.Ctx) (*User, error) {
	token := c.Cookies(SessionCookie)
	if token == "" {
		return nil, nil
	}
	id, err := h.tokens.verify(token, time.Now())
	if err != nil {
		return nil, nil
	}
	u, err := h.FindByID(c.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (h *Handler) Logout(c fiber.Ctx) error {
	h.ClearSession(c)
	return c.Redirect().To("/")
}

func (h *Handler) ClearSession(c fiber.Ctx) {
	h.setSession(c, "", time.Time{})
}

func (h *Handler) setSession(c fiber.Ctx, token string, expires time.Time) {
	cookie := &fiber.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		Secure:   h.secureCookie,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	}
	if token == "" {
		cookie.MaxAge = -1
		cookie.Expires = time.Unix(0, 0)
	}
	c.Cookie(cookie)
}
