package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/config"
	"git.shungites.casa/tiny/rewards/internal/database"
	"git.shungites.casa/tiny/rewards/internal/sounds"
)

const (
	multipartOverhead = 1 << 20
	dbStartupTimeout  = 30 * time.Second
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

	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	soundHandler, err := sounds.NewHandler(sounds.Options{
		DB:          db,
		Directory:   cfg.Files.Directory,
		MaxSize:     cfg.Files.MaxSize,
		MaxDuration: cfg.Files.MaxDuration,
		Deduplicate: cfg.Files.Deduplicate,
	})
	if err != nil {
		log.Fatalf("sounds: %v", err)
	}
	soundHandler.Register(app)

	log.Fatal(app.Listen(cfg.Server.Address))
}
