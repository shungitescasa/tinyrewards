package account

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"

	"github.com/gofiber/fiber/v3"
)

const (
	CSRFField  = "csrf"
	CSRFHeader = "X-CSRF-Token"
)

func (h *Handler) CSRFToken(c fiber.Ctx) string {
	session := c.Cookies(SessionCookie)
	if session == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(h.csrfMAC(session))
}

func (h *Handler) CSRF(c fiber.Ctx) error {
	if c.Get(fiber.HeaderAuthorization) != "" {
		return c.Next()
	}
	session := c.Cookies(SessionCookie)
	if session == "" {
		return c.Next()
	}
	token := c.Get(CSRFHeader)
	if token == "" {
		token = c.FormValue(CSRFField)
	}
	mac, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || !hmac.Equal(mac, h.csrfMAC(session)) {
		return fiber.NewError(fiber.StatusForbidden, "invalid or missing csrf token")
	}
	return c.Next()
}

func (h *Handler) csrfMAC(session string) []byte {
	m := hmac.New(sha256.New, h.tokens.secret)
	m.Write([]byte("csrf\x00"))
	m.Write([]byte(session))
	return m.Sum(nil)
}
