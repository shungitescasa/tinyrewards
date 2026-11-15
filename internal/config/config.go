package config

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

const DefaultPath = "tinyrewards.ini"

type Server struct {
	Address string
}

type Sounds struct {
	Directory   string
	MaxSize     int64
	MaxDuration time.Duration
}

type Config struct {
	Server Server
	Sounds Sounds
}

func Default() Config {
	return Config{
		Server: Server{
			Address: ":3000",
		},
		Sounds: Sounds{
			Directory:   "userdata/sounds",
			MaxSize:     16 << 20,
			MaxDuration: 10 * time.Second,
		},
	}
}

func Load(path string, required bool) (Config, error) {
	cfg := Default()

	file, err := ini.Load(path)
	if err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("load %s: %w", path, err)
	}

	server := file.Section("server")
	cfg.Server.Address = server.Key("address").MustString(cfg.Server.Address)

	sounds := file.Section("sounds")
	cfg.Sounds.Directory = sounds.Key("directory").MustString(cfg.Sounds.Directory)

	if key := sounds.Key("max_size"); key.String() != "" {
		size, err := ParseSize(key.String())
		if err != nil {
			return cfg, fmt.Errorf("%s: sounds.max_size: %w", path, err)
		}
		cfg.Sounds.MaxSize = size
	}

	if key := sounds.Key("max_duration"); key.String() != "" {
		d, err := time.ParseDuration(key.String())
		if err != nil {
			return cfg, fmt.Errorf("%s: sounds.max_duration: %w", path, err)
		}
		cfg.Sounds.MaxDuration = d
	}

	if err := cfg.validate(); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c Config) validate() error {
	switch {
	case c.Server.Address == "":
		return errors.New("server.address must not be empty")
	case c.Sounds.Directory == "":
		return errors.New("sounds.directory must not be empty")
	case c.Sounds.MaxSize <= 0:
		return errors.New("sounds.max_size must be positive")
	case c.Sounds.MaxDuration <= 0:
		return errors.New("sounds.max_duration must be positive")
	}
	return nil
}

var sizeUnits = []struct {
	suffix string
	factor int64
}{
	{"GB", 1 << 30},
	{"MB", 1 << 20},
	{"KB", 1 << 10},
	{"G", 1 << 30},
	{"M", 1 << 20},
	{"K", 1 << 10},
	{"B", 1},
}

func ParseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	factor := int64(1)
	for _, u := range sizeUnits {
		if strings.HasSuffix(s, u.suffix) {
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			factor = u.factor
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	if n < 0 || n > (1<<62)/factor {
		return 0, fmt.Errorf("size %q out of range", s)
	}
	return n * factor, nil
}
