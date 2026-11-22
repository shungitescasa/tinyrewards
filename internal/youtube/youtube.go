package youtube

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const idLength = 11

var ErrInvalidLink = errors.New("not a YouTube video link")

type Video struct {
	ID    string `json:"id"`
	Start int    `json:"start"`
}

func Parse(input string) (Video, error) {
	fields := strings.Fields(input)
	if len(fields) != 1 {
		return Video{}, ErrInvalidLink
	}
	raw := fields[0]
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Video{}, ErrInvalidLink
	}

	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	query := u.Query()

	var id string
	switch host {
	case "youtu.be":
		id = segments[0]
	case "youtube.com", "m.youtube.com", "music.youtube.com", "youtube-nocookie.com":
		switch {
		case len(segments) == 1 && segments[0] == "watch":
			id = query.Get("v")
		case len(segments) == 2 && (segments[0] == "shorts" || segments[0] == "embed" || segments[0] == "live" || segments[0] == "v"):
			id = segments[1]
		}
	}
	if !isID(id) {
		return Video{}, ErrInvalidLink
	}
	return Video{ID: id, Start: parseStart(query.Get("t"), query.Get("start"))}, nil
}

func isID(s string) bool {
	if len(s) != idLength {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func parseStart(values ...string) int {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(strings.TrimSuffix(v, "s")); err == nil && n >= 0 {
			return n
		}
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			return int(d / time.Second)
		}
	}
	return 0
}
