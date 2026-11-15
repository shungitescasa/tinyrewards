package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"
)

const DefaultPath = "tinyrewards.ini"

const (
	StorageLocal = "local"
	StorageS3    = "s3"
)

type Server struct {
	Address string
}

type Files struct {
	Storage     string
	Directory   string
	MaxSize     int64
	MaxDuration time.Duration
	Deduplicate bool
}

type Database struct {
	DSN string
}

type S3 struct {
	Endpoint       string
	Secure         bool
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
	Prefix         string
	ForcePathStyle bool
}

type Config struct {
	Server   Server
	Database Database
	Files    Files
	S3       S3
}

func Default() Config {
	return Config{
		Server: Server{
			Address: ":3000",
		},
		Database: Database{
			DSN: "tinyrewards:tinyrewards@tcp(127.0.0.1:3306)/tinyrewards",
		},
		Files: Files{
			Storage:     StorageLocal,
			Directory:   "userdata/files",
			MaxSize:     16 << 20,
			MaxDuration: 10 * time.Second,
			Deduplicate: true,
		},
		S3: S3{
			Endpoint: "s3.amazonaws.com",
			Secure:   true,
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

	cfg.Database.DSN = file.Section("database").Key("dsn").MustString(cfg.Database.DSN)

	files := file.Section("files")
	cfg.Files.Storage = strings.ToLower(files.Key("storage").MustString(cfg.Files.Storage))
	cfg.Files.Directory = files.Key("directory").MustString(cfg.Files.Directory)

	if key := files.Key("max_size"); key.String() != "" {
		size, err := ParseSize(key.String())
		if err != nil {
			return cfg, fmt.Errorf("%s: files.max_size: %w", path, err)
		}
		cfg.Files.MaxSize = size
	}

	if key := files.Key("max_duration"); key.String() != "" {
		d, err := time.ParseDuration(key.String())
		if err != nil {
			return cfg, fmt.Errorf("%s: files.max_duration: %w", path, err)
		}
		cfg.Files.MaxDuration = d
	}

	if key := files.Key("deduplicate"); key.String() != "" {
		dedup, err := key.Bool()
		if err != nil {
			return cfg, fmt.Errorf("%s: files.deduplicate: %w", path, err)
		}
		cfg.Files.Deduplicate = dedup
	}

	s3 := file.Section("s3")
	if key := s3.Key("endpoint"); key.String() != "" {
		host, secure, err := parseEndpoint(key.String())
		if err != nil {
			return cfg, fmt.Errorf("%s: s3.endpoint: %w", path, err)
		}
		cfg.S3.Endpoint = host
		cfg.S3.Secure = secure
	}
	cfg.S3.Region = s3.Key("region").MustString(cfg.S3.Region)
	cfg.S3.Bucket = s3.Key("bucket").MustString(cfg.S3.Bucket)
	cfg.S3.AccessKey = s3.Key("access_key").MustString(cfg.S3.AccessKey)
	cfg.S3.SecretKey = s3.Key("secret_key").MustString(cfg.S3.SecretKey)
	cfg.S3.Prefix = strings.Trim(s3.Key("prefix").MustString(cfg.S3.Prefix), "/")

	if key := s3.Key("force_path_style"); key.String() != "" {
		pathStyle, err := key.Bool()
		if err != nil {
			return cfg, fmt.Errorf("%s: s3.force_path_style: %w", path, err)
		}
		cfg.S3.ForcePathStyle = pathStyle
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
	case c.Database.DSN == "":
		return errors.New("database.dsn must not be empty")
	case c.Files.MaxSize <= 0:
		return errors.New("files.max_size must be positive")
	case c.Files.MaxDuration <= 0:
		return errors.New("files.max_duration must be positive")
	}

	switch c.Files.Storage {
	case StorageLocal:
		if c.Files.Directory == "" {
			return errors.New("files.directory must not be empty")
		}
	case StorageS3:
		switch {
		case c.S3.Endpoint == "":
			return errors.New("s3.endpoint must not be empty")
		case c.S3.Bucket == "":
			return errors.New("s3.bucket must not be empty")
		case (c.S3.AccessKey == "") != (c.S3.SecretKey == ""):
			return errors.New("s3.access_key and s3.secret_key must be set together")
		}
	default:
		return fmt.Errorf("files.storage must be %q or %q, got %q", StorageLocal, StorageS3, c.Files.Storage)
	}
	return nil
}

func parseEndpoint(s string) (string, bool, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		return s, true, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", false, err
	}
	if u.Host == "" {
		return "", false, fmt.Errorf("missing host in %q", s)
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, fmt.Errorf("endpoint %q must not contain a path", s)
	}
	switch u.Scheme {
	case "https":
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	}
	return "", false, fmt.Errorf("unsupported scheme %q", u.Scheme)
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
