package account

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/twitch"
)

const (
	twitchStateCookie = "twitch_oauth_state"
	stateTTL          = 10 * time.Minute
	twitchTimeout     = 15 * time.Second
)

type TokenStore interface {
	SaveToken(ctx context.Context, userID, twitchID string, token *oauth2.Token) error
}

type TwitchOptions struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
	Tokens       TokenStore
}

type Options struct {
	DB          *sql.DB
	Twitch      *TwitchOptions
	TokenSecret []byte
	TokenTTL    time.Duration
}

type Handler struct {
	db           *sql.DB
	tokens       *tokens
	twitch       *oauth2.Config
	twitchTokens TokenStore
	secureCookie bool
	client       *http.Client
}

func NewHandler(opts Options) *Handler {
	h := &Handler{
		db:     opts.DB,
		tokens: &tokens{secret: opts.TokenSecret, ttl: opts.TokenTTL},
		client: &http.Client{Timeout: twitchTimeout},
	}
	if opts.Twitch != nil {
		h.twitch = &oauth2.Config{
			ClientID:     opts.Twitch.ClientID,
			ClientSecret: opts.Twitch.ClientSecret,
			RedirectURL:  opts.Twitch.RedirectURL,
			Scopes:       opts.Twitch.Scopes,
			Endpoint:     twitch.Endpoint,
		}
		h.twitchTokens = opts.Twitch.Tokens
		if u, err := url.Parse(opts.Twitch.RedirectURL); err == nil {
			h.secureCookie = u.Scheme == "https"
		}
	}
	return h
}

func (h *Handler) TwitchEnabled() bool {
	return h.twitch != nil
}

func (h *Handler) LoginTwitch(c fiber.Ctx) error {
	if reason := c.Query("error"); reason != "" {
		h.clearState(c)
		return fiber.NewError(fiber.StatusUnauthorized, "twitch authorization failed: "+c.Query("error_description", reason))
	}

	code := c.Query("code")
	if code == "" {
		state := rand.Text()
		c.Cookie(&fiber.Cookie{
			Name:     twitchStateCookie,
			Value:    state,
			Path:     c.Path(),
			MaxAge:   int(stateTTL.Seconds()),
			Secure:   h.secureCookie,
			HTTPOnly: true,
			SameSite: fiber.CookieSameSiteLaxMode,
		})
		return c.Redirect().To(h.twitch.AuthCodeURL(state))
	}

	expected := c.Cookies(twitchStateCookie)
	h.clearState(c)
	if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(c.Query("state"))) != 1 {
		return fiber.NewError(fiber.StatusBadRequest, "invalid or expired oauth state")
	}

	tw, twitchToken, err := h.fetchTwitchUser(c.Context(), code)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return fiber.NewError(fiber.StatusUnauthorized, "twitch rejected the authorization code")
		}
		log.Printf("account: twitch login: %v", err)
		return fiber.ErrBadGateway
	}

	u, err := h.login(c.Context(), PlatformTwitch, tw.ID, tw.Login)
	if err != nil {
		if errors.Is(err, ErrUsernameTaken) {
			return fiber.NewError(fiber.StatusConflict, err.Error())
		}
		log.Printf("account: twitch login: %v", err)
		return fiber.ErrInternalServerError
	}

	if h.twitchTokens != nil {
		if err := h.twitchTokens.SaveToken(c.Context(), u.ID, tw.ID, twitchToken); err != nil {
			log.Printf("account: saving twitch token: %v", err)
			return fiber.ErrInternalServerError
		}
	}

	token, expires, err := h.tokens.issue(u.ID, time.Now())
	if err != nil {
		log.Printf("account: issuing token: %v", err)
		return fiber.ErrInternalServerError
	}

	h.setSession(c, token, expires)
	return c.Redirect().To("/account")
}

func (h *Handler) clearState(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     twitchStateCookie,
		Path:     c.Path(),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		Secure:   h.secureCookie,
		HTTPOnly: true,
		SameSite: fiber.CookieSameSiteLaxMode,
	})
}
