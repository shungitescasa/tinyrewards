package main

import (
	"flag"
	"log"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/config"
	"git.shungites.casa/tiny/rewards/internal/sounds"
)

const multipartOverhead = 1 << 20

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

	app := fiber.New(fiber.Config{
		BodyLimit: int(cfg.Sounds.MaxSize + multipartOverhead),
	})

	app.Get("/", func(c fiber.Ctx) error {
		return c.SendString("Hello, World!")
	})

	soundHandler, err := sounds.NewHandler(sounds.Options{
		Directory:   cfg.Sounds.Directory,
		MaxSize:     cfg.Sounds.MaxSize,
		MaxDuration: cfg.Sounds.MaxDuration,
	})
	if err != nil {
		log.Fatalf("sounds: %v", err)
	}
	soundHandler.Register(app)

	log.Fatal(app.Listen(cfg.Server.Address))
}
