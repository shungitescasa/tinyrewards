package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/config"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/middleware"
	"git.shungites.casa/tiny/rewards/internal/rewards"
	"git.shungites.casa/tiny/rewards/internal/sounds"
	"git.shungites.casa/tiny/rewards/internal/twitch"
)

//go:embed templates static
var files embed.FS

const assetMaxAge = 365 * 24 * 60 * 60

var pages = []string{"index", "sounds", "upload", "rewards", "reward", "login", "account", "account_delete", "user", "approval", "error"}

var funcs = template.FuncMap{
	"date": func(t time.Time) string {
		return t.Format("2006-01-02")
	},
	"dict": func(pairs ...any) (map[string]any, error) {
		if len(pairs)%2 != 0 {
			return nil, errors.New("dict: odd number of arguments")
		}
		m := make(map[string]any, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			key, ok := pairs[i].(string)
			if !ok {
				return nil, fmt.Errorf("dict: key %v is not a string", pairs[i])
			}
			m[key] = pairs[i+1]
		}
		return m, nil
	},
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	"emote": func(data string) string {
		provider, id, _ := strings.Cut(data, ":")
		return emotes.ImageURL(provider, id)
	},
}

type Options struct {
	Accounts         *account.Handler
	Sounds           *sounds.Handler
	Rewards          *rewards.Handler
	Emotes           *emotes.Checker
	Twitch           *twitch.Client
	Instance         config.Instance
	PublicURL        string
	MaxSize          int64
	MaxDuration      time.Duration
	AnonymousUploads bool
}

type Handler struct {
	opts      Options
	templates map[string]*template.Template
	assets    map[string]string
}

func NewHandler(opts Options) *Handler {
	h := &Handler{
		opts:      opts,
		templates: make(map[string]*template.Template, len(pages)),
		assets:    map[string]string{},
	}

	err := fs.WalkDir(files, "static", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		h.assets[strings.TrimPrefix(path, "static/")] = "/" + path + "?v=" + hex.EncodeToString(sum[:6])
		return nil
	})
	if err != nil {
		panic(err)
	}

	for _, name := range pages {
		h.templates[name] = template.Must(template.New(name).
			Funcs(funcs).
			Funcs(template.FuncMap{"asset": h.asset}).
			ParseFS(files, "templates/layout.html", "templates/partials.html", "templates/"+name+".html"))
	}
	return h
}

func (h *Handler) asset(name string) (string, error) {
	if link, ok := h.assets[name]; ok {
		return link, nil
	}
	return "", fmt.Errorf("unknown asset %q", name)
}

func (h *Handler) Register(app fiber.Router) {
	app.Get("/static/*", static.New("static", static.Config{FS: files, MaxAge: assetMaxAge}))

	app.Get("/", h.index)
	app.Get("/sounds", middleware.Negotiate(h.soundList, h.opts.Sounds.List))
	app.Get("/sounds/upload", h.uploadPage)
	app.Post("/sounds/upload", sameOrigin, middleware.Negotiate(h.upload, h.opts.Sounds.Upload))
	app.Post("/sounds/delete", h.opts.Accounts.CSRF, middleware.Negotiate(h.deleteSound, h.opts.Sounds.Delete))
	app.Delete("/sounds/delete", h.opts.Accounts.CSRF, middleware.Negotiate(h.deleteSound, h.opts.Sounds.Delete))
	app.Get("/rewards", middleware.Negotiate(h.rewardList, h.opts.Rewards.Mine))
	app.Get("/rewards/new", h.rewardPage)
	app.Post("/rewards/new", sameOrigin, middleware.Negotiate(h.createReward, h.opts.Rewards.Add))
	app.Post("/rewards/:id/delete", sameOrigin, middleware.Negotiate(h.deleteReward, h.opts.Rewards.Delete))
	app.Delete("/rewards/:id", sameOrigin, middleware.Negotiate(h.deleteReward, h.opts.Rewards.Delete))
	app.Get("/account", middleware.Negotiate(h.accountPage, h.opts.Accounts.Me))
	app.Get("/account/login", h.loginPage)
	if h.opts.Accounts.TwitchEnabled() {
		app.Get("/account/login/twitch", h.opts.Accounts.LoginTwitch)
	}
	app.Post("/account/logout", sameOrigin, h.opts.Accounts.Logout)
	app.Get("/account/delete", h.deleteAccountPage)
	app.Post("/account/delete", sameOrigin, middleware.Negotiate(h.deleteAccount, h.opts.Accounts.Delete))
	app.Delete("/account/delete", sameOrigin, middleware.Negotiate(h.deleteAccount, h.opts.Accounts.Delete))
	app.Get("/system/approval", middleware.Negotiate(h.approvalPage, h.approvalJSON))
	app.Post("/system/approval", h.opts.Accounts.CSRF, middleware.Negotiate(h.moderate, h.moderateJSON))
	app.Get("/users/:id", middleware.Negotiate(h.userPage, h.userJSON))
}

func (h *Handler) Error(c fiber.Ctx, err error) error {
	code, message := fiber.StatusInternalServerError, ""
	var fe *fiber.Error
	if errors.As(err, &fe) {
		code, message = fe.Code, fe.Message
	} else {
		log.Printf("%s %s: %v", c.Method(), c.Path(), err)
	}
	status := http.StatusText(code)
	if message == "" {
		message = status
	}
	return h.render(c, code, "error", fiber.Map{"Code": code, "Status": status, "Message": message})
}

func (h *Handler) render(c fiber.Ctx, status int, name string, data fiber.Map) error {
	if data == nil {
		data = fiber.Map{}
	}
	data["Instance"] = h.opts.Instance
	data["Viewer"] = h.viewer(c)
	data["CSRF"] = h.opts.Accounts.CSRFToken(c)
	data["Messages"] = c.Redirect().Messages()

	var buf bytes.Buffer
	if err := h.templates[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return c.Status(status).Send(buf.Bytes())
}

type viewerKey struct{}

func (h *Handler) viewer(c fiber.Ctx) *account.User {
	if u, ok := c.Locals(viewerKey{}).(*account.User); ok {
		return u
	}
	u, err := h.opts.Accounts.SessionUser(c)
	if err != nil {
		log.Printf("web: loading session: %v", err)
	}
	c.Locals(viewerKey{}, u)
	return u
}

func (h *Handler) baseURL(c fiber.Ctx) string {
	return sounds.BaseURL(c, h.opts.PublicURL)
}

func (h *Handler) twitchID(ctx context.Context, userID string) string {
	conns, err := h.opts.Accounts.Connections(ctx, userID)
	if err != nil {
		log.Printf("web: loading connections: %v", err)
	}
	for _, conn := range conns {
		if conn.Platform == account.PlatformTwitch {
			return conn.AliasID
		}
	}
	return ""
}

func (h *Handler) channelEmotes(ctx context.Context, twitchID string) []emotes.Emote {
	list, err := h.opts.Emotes.List(ctx, twitchID)
	if err != nil {
		log.Printf("web: loading emotes: %v", err)
	}
	return list
}

func sameOrigin(c fiber.Ctx) error {
	switch c.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return c.Next()
	}
	return fiber.NewError(fiber.StatusForbidden, "cross-site form submissions are not allowed")
}

func toLogin(c fiber.Ctx) error {
	return c.Redirect().To("/account/login")
}

func userError(err error) (int, string, bool) {
	var fe *fiber.Error
	if errors.As(err, &fe) && fe.Code < fiber.StatusInternalServerError {
		return fe.Code, fe.Message, true
	}
	return 0, "", false
}

type pager struct {
	Page int64
	Prev string
	Next string
}

func newPager(path string, query url.Values, page int64, more bool) pager {
	link := func(n int64) string {
		query.Set("p", strconv.FormatInt(n, 10))
		return path + "?" + query.Encode()
	}
	p := pager{Page: page}
	if page > 1 {
		p.Prev = link(page - 1)
	}
	if more {
		p.Next = link(page + 1)
	}
	return p
}

func pageNumber(c fiber.Ctx) int64 {
	if p, err := strconv.ParseInt(c.Query("p"), 10, 64); err == nil && p > 0 {
		return p
	}
	return 1
}

func formatSize(n int64) string {
	return strconv.FormatFloat(math.Round(float64(n)/(1<<20)*10)/10, 'f', -1, 64) + " MB"
}
