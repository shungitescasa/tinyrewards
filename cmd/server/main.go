package main

import (
	"context"
	"crypto/rand"
	"flag"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
	"git.shungites.casa/tiny/rewards/internal/config"
	"git.shungites.casa/tiny/rewards/internal/database"
	"git.shungites.casa/tiny/rewards/internal/emotes"
	"git.shungites.casa/tiny/rewards/internal/middleware"
	"git.shungites.casa/tiny/rewards/internal/rewards"
	"git.shungites.casa/tiny/rewards/internal/sounds"
	"git.shungites.casa/tiny/rewards/internal/storage"
	"git.shungites.casa/tiny/rewards/internal/web"
	"git.shungites.casa/tiny/rewards/internal/widget"
)

const (
	multipartOverhead = 1 << 20
	dbStartupTimeout  = 30 * time.Second
	s3StartupTimeout  = 30 * time.Second
	apiPrefix         = "/api"
)

func main() {
	configPath := flag.String("config", config.DefaultPath, "path to the ini configuration file")
	flag.Parse()

	explicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			explicit = true
		}
	})

	cfg, err := config.Load(*configPath, explicit)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dbStartupTimeout)
	db, err := database.Open(ctx, cfg.Database.DSN)
	cancel()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	store, err := openStorage(cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	var twitch *account.TwitchOptions
	var twitchCredentials *emotes.TwitchCredentials
	if cfg.Twitch.Enabled() {
		twitchCredentials = &emotes.TwitchCredentials{
			ClientID:     cfg.Twitch.ClientID,
			ClientSecret: cfg.Twitch.ClientSecret,
		}
		twitch = &account.TwitchOptions{
			ClientID:     cfg.Twitch.ClientID,
			ClientSecret: cfg.Twitch.ClientSecret,
			RedirectURL:  cfg.Twitch.RedirectURL,
		}
	} else {
		log.Print("twitch: client_id is not set, twitch login and twitch emote rewards are disabled")
	}
	log.Printf("emotes: enabled providers: %s", strings.Join(cfg.Emotes.Providers, ", "))
	secret := []byte(cfg.Auth.Secret)
	if len(secret) == 0 {
		secret = make([]byte, config.MinSecretLength)
		rand.Read(secret)
		log.Print("auth: secret is not set, using a random one; tokens will not survive a restart")
	}
	accounts := account.NewHandler(account.Options{
		DB:          db,
		Twitch:      twitch,
		TokenSecret: secret,
		TokenTTL:    cfg.Auth.TokenTTL,
	})

	soundHandler := sounds.NewHandler(sounds.Options{
		DB:               db,
		Storage:          store,
		MaxSize:          cfg.Files.MaxSize,
		MaxDuration:      cfg.Files.MaxDuration,
		Deduplicate:      cfg.Files.Deduplicate,
		PageSize:         cfg.Sounds.PageSize,
		PublicURL:        cfg.Server.PublicURL,
		AnonymousUploads: cfg.Sounds.AnonymousUploads,
		Authenticate:     accounts.OptionalAuthenticate,
	})

	emoteChecker := emotes.NewChecker(cfg.Emotes.Providers, twitchCredentials)

	rewardHandler := rewards.NewHandler(rewards.Options{
		DB:           db,
		Authenticate: accounts.Authenticate,
		Emotes:       emoteChecker,
		PublicURL:    cfg.Server.PublicURL,
	})

	widgetHandler := widget.NewHandler(widget.Options{
		DB:     db,
		Emotes: emoteChecker,
	})

	pages := web.NewHandler(web.Options{
		Accounts:         accounts,
		Sounds:           soundHandler,
		Rewards:          rewardHandler,
		Emotes:           emoteChecker,
		PublicURL:        cfg.Server.PublicURL,
		MaxSize:          cfg.Files.MaxSize,
		MaxDuration:      cfg.Files.MaxDuration,
		AnonymousUploads: cfg.Sounds.AnonymousUploads,
	})

	app := fiber.New(fiber.Config{
		BodyLimit: int(cfg.Files.MaxSize + multipartOverhead),
		ErrorHandler: func(c fiber.Ctx, err error) error {
			if p := c.Path(); p == apiPrefix || strings.HasPrefix(p, apiPrefix+"/") {
				return middleware.WriteError(c, err)
			}
			return pages.Error(c, err)
		},
	})

	api := app.Group(apiPrefix, middleware.JSON())
	accounts.Register(api)
	soundHandler.Register(app, api)
	rewardHandler.Register(api)
	widgetHandler.Register(app, api)
	pages.Register(app)

	log.Fatal(app.Listen(cfg.Server.Address))
}

func openStorage(cfg config.Config) (storage.Storage, error) {
	if cfg.Files.Storage != config.StorageS3 {
		return storage.NewLocal(cfg.Files.Directory)
	}

	ctx, cancel := context.WithTimeout(context.Background(), s3StartupTimeout)
	defer cancel()
	return storage.NewS3(ctx, storage.S3Options{
		Endpoint:       cfg.S3.Endpoint,
		Secure:         cfg.S3.Secure,
		Region:         cfg.S3.Region,
		Bucket:         cfg.S3.Bucket,
		AccessKey:      cfg.S3.AccessKey,
		SecretKey:      cfg.S3.SecretKey,
		Prefix:         cfg.S3.Prefix,
		ForcePathStyle: cfg.S3.ForcePathStyle,
	})
}
