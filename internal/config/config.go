package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/ini.v1"

	"git.shungites.casa/tiny/rewards/internal/emotes"
)

const DefaultPath = "tinyrewards.ini"

const MinSecretLength = 32

const (
	StorageLocal = "local"
	StorageS3    = "s3"
)

type Server struct {
	Address   string
	PublicURL string
}

type Files struct {
	Storage     string
	Directory   string
	MaxSize     int64
	MaxDuration time.Duration
	Deduplicate bool
}

type Sounds struct {
	PageSize         int
	AnonymousUploads bool
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

type Twitch struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

func (t Twitch) Enabled() bool {
	return t.ClientID != ""
}

type Emotes struct {
	Providers []string
}

type Anonupload struct {
	URL string
}

func (a Anonupload) Enabled() bool {
	return a.URL != ""
}

type Auth struct {
	Secret   string
	TokenTTL time.Duration
}

type Instance struct {
	Name           string
	Subtitle       string
	MaintainerName string
	MaintainerURL  string
	TOS            string
	PrivacyPolicy  string
}

type Config struct {
	Instance   Instance
	Server     Server
	Database   Database
	Files      Files
	Sounds     Sounds
	S3         S3
	Twitch     Twitch
	Emotes     Emotes
	Anonupload Anonupload
	Auth       Auth
}

func Default() Config {
	return Config{
		Instance: Instance{
			Name:     "TinyRewards",
			Subtitle: "Sound alerts for your Twitch chat.",
		},
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
		Sounds: Sounds{
			PageSize:         20,
			AnonymousUploads: true,
		},
		S3: S3{
			Endpoint: "s3.amazonaws.com",
			Secure:   true,
		},
		Emotes: Emotes{
			Providers: []string{emotes.ProviderSevenTV, emotes.ProviderBTTV, emotes.ProviderFFZ},
		},
		Auth: Auth{
			TokenTTL: 7 * 24 * time.Hour,
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

	instance := file.Section("instance")
	cfg.Instance.Name = strings.TrimSpace(instance.Key("name").MustString(cfg.Instance.Name))
	cfg.Instance.Subtitle = strings.TrimSpace(instance.Key("subtitle").MustString(cfg.Instance.Subtitle))
	cfg.Instance.MaintainerName = strings.TrimSpace(instance.Key("maintainer_name").MustString(cfg.Instance.MaintainerName))
	cfg.Instance.MaintainerURL = strings.TrimSpace(instance.Key("maintainer_url").MustString(cfg.Instance.MaintainerURL))
	cfg.Instance.TOS = strings.TrimSpace(instance.Key("tos").MustString(cfg.Instance.TOS))
	cfg.Instance.PrivacyPolicy = strings.TrimSpace(instance.Key("privacy_policy").MustString(cfg.Instance.PrivacyPolicy))

	server := file.Section("server")
	cfg.Server.Address = server.Key("address").MustString(cfg.Server.Address)
	cfg.Server.PublicURL = strings.TrimRight(strings.TrimSpace(server.Key("public_url").MustString(cfg.Server.PublicURL)), "/")

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

	sounds := file.Section("sounds")
	if key := sounds.Key("anonymous_uploads"); key.String() != "" {
		anonymous, err := key.Bool()
		if err != nil {
			return cfg, fmt.Errorf("%s: sounds.anonymous_uploads: %w", path, err)
		}
		cfg.Sounds.AnonymousUploads = anonymous
	}

	if key := sounds.Key("page_size"); key.String() != "" {
		pageSize, err := key.Int()
		if err != nil {
			return cfg, fmt.Errorf("%s: sounds.page_size: %w", path, err)
		}
		cfg.Sounds.PageSize = pageSize
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

	twitch := file.Section("twitch")
	cfg.Twitch.ClientID = twitch.Key("client_id").MustString(cfg.Twitch.ClientID)
	cfg.Twitch.ClientSecret = twitch.Key("client_secret").MustString(cfg.Twitch.ClientSecret)
	cfg.Twitch.RedirectURL = twitch.Key("redirect_url").MustString(cfg.Twitch.RedirectURL)

	emoteSection := file.Section("emotes")
	if emoteSection.HasKey("providers") {
		cfg.Emotes.Providers = parseList(emoteSection.Key("providers").String())
	} else if cfg.Twitch.Enabled() {
		cfg.Emotes.Providers = append(cfg.Emotes.Providers, emotes.ProviderTwitch)
	}

	cfg.Anonupload.URL = strings.TrimRight(strings.TrimSpace(file.Section("anonupload").Key("url").MustString(cfg.Anonupload.URL)), "/")

	auth := file.Section("auth")
	cfg.Auth.Secret = auth.Key("secret").MustString(cfg.Auth.Secret)
	if key := auth.Key("token_ttl"); key.String() != "" {
		d, err := time.ParseDuration(key.String())
		if err != nil {
			return cfg, fmt.Errorf("%s: auth.token_ttl: %w", path, err)
		}
		cfg.Auth.TokenTTL = d
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
	case c.Sounds.PageSize <= 0:
		return errors.New("sounds.page_size must be positive")
	case c.Auth.TokenTTL <= 0:
		return errors.New("auth.token_ttl must be positive")
	case c.Auth.Secret != "" && len(c.Auth.Secret) < MinSecretLength:
		return fmt.Errorf("auth.secret must be at least %d bytes", MinSecretLength)
	}

	if c.Instance.Name == "" {
		return errors.New("instance.name must not be empty")
	}
	for key, value := range map[string]string{
		"instance.maintainer_url": c.Instance.MaintainerURL,
		"instance.tos":            c.Instance.TOS,
		"instance.privacy_policy": c.Instance.PrivacyPolicy,
	} {
		if value == "" {
			continue
		}
		u, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%s must be an absolute http(s) URL, got %q", key, value)
		}
	}

	if c.Server.PublicURL != "" {
		u, err := url.Parse(c.Server.PublicURL)
		if err != nil {
			return fmt.Errorf("server.public_url: %w", err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("server.public_url must be an absolute http(s) URL, got %q", c.Server.PublicURL)
		}
	}

	if c.Anonupload.URL != "" {
		u, err := url.Parse(c.Anonupload.URL)
		if err != nil {
			return fmt.Errorf("anonupload.url: %w", err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("anonupload.url must be an absolute http(s) URL, got %q", c.Anonupload.URL)
		}
	}

	if err := c.Twitch.validate(); err != nil {
		return err
	}

	for _, p := range c.Emotes.Providers {
		if !emotes.IsProvider(p) {
			return fmt.Errorf("emotes.providers: unknown provider %q, must be one of: %s", p, strings.Join(emotes.Providers, ", "))
		}
		if p == emotes.ProviderTwitch && !c.Twitch.Enabled() {
			return errors.New("emotes.providers: twitch requires the [twitch] section to be configured")
		}
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

func (t Twitch) validate() error {
	if t.ClientID == "" && t.ClientSecret == "" && t.RedirectURL == "" {
		return nil
	}
	switch {
	case t.ClientID == "":
		return errors.New("twitch.client_id must not be empty")
	case t.ClientSecret == "":
		return errors.New("twitch.client_secret must not be empty")
	case t.RedirectURL == "":
		return errors.New("twitch.redirect_url must not be empty")
	}
	u, err := url.Parse(t.RedirectURL)
	if err != nil {
		return fmt.Errorf("twitch.redirect_url: %w", err)
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("twitch.redirect_url must be an absolute http(s) URL, got %q", t.RedirectURL)
	}
	return nil
}

func parseList(s string) []string {
	out := []string{}
	for _, item := range strings.Split(s, ",") {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
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
