package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/config"
	"git.shungites.casa/tiny/rewards/internal/database"
	"git.shungites.casa/tiny/rewards/internal/middleware"
	"git.shungites.casa/tiny/rewards/internal/sounds"
	"git.shungites.casa/tiny/rewards/internal/storage"
)

const (
	multipartOverhead = 1 << 20
	dbStartupTimeout  = 30 * time.Second
	s3StartupTimeout  = 30 * time.Second
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

	app := fiber.New(fiber.Config{
		BodyLimit: int(cfg.Files.MaxSize + multipartOverhead),
	})

	app.Use(middleware.JSON())

	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	store, err := openStorage(cfg)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	sounds.NewHandler(sounds.Options{
		DB:          db,
		Storage:     store,
		MaxSize:     cfg.Files.MaxSize,
		MaxDuration: cfg.Files.MaxDuration,
		Deduplicate: cfg.Files.Deduplicate,
		PageSize:    cfg.Sounds.PageSize,
	}).Register(app)

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
