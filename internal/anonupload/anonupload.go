package anonupload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	requestTimeout  = 10 * time.Second
	maxResponseSize = 1 << 20
	maxAttachments  = 32
	userAgent       = "TinyRewards"
)

var (
	ErrNoPost  = errors.New("anonupload: random post link is missing")
	ErrNoMedia = errors.New("anonupload: random post has no attachments")
)

type Media struct {
	URL  string `json:"url"`
	Mime string `json:"mime"`
}

type post struct {
	Data struct {
		URLs struct {
			DownloadURL string `json:"download_url"`
		} `json:"urls"`
		Attachments []struct {
			Mime string `json:"mime"`
			URLs struct {
				DownloadURL string `json:"download_url"`
			} `json:"urls"`
		} `json:"attachments"`
	} `json:"data"`
}

type Client struct {
	baseURL  string
	http     *http.Client
	noFollow *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: requestTimeout},
		noFollow: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (c *Client) Random(ctx context.Context) ([]Media, error) {
	link, err := c.randomLink(ctx)
	if err != nil {
		return nil, err
	}
	res, err := c.get(ctx, c.http, link.String())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anonupload: post %s: unexpected status %s", link, res.Status)
	}
	p, err := decode(res)
	if err != nil {
		return nil, err
	}
	return attachments(res.Request.URL, p)
}

func (c *Client) randomLink(ctx context.Context) (*url.URL, error) {
	res, err := c.get(ctx, c.noFollow, c.baseURL+"/?random")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	var raw string
	switch {
	case res.StatusCode >= 300 && res.StatusCode < 400:
		raw = res.Header.Get("Location")
	case res.StatusCode == http.StatusOK:
		p, err := decode(res)
		if err != nil {
			return nil, err
		}
		raw = p.Data.URLs.DownloadURL
	default:
		return nil, fmt.Errorf("anonupload: random post: unexpected status %s", res.Status)
	}
	if raw == "" {
		return nil, ErrNoPost
	}
	return resolve(res.Request.URL, raw)
}

func (c *Client) get(ctx context.Context, client *http.Client, link string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anonupload: requesting %s: %w", link, err)
	}
	return res, nil
}

func decode(res *http.Response) (post, error) {
	var p post
	if err := json.NewDecoder(io.LimitReader(res.Body, maxResponseSize)).Decode(&p); err != nil {
		return p, fmt.Errorf("anonupload: decoding %s: %w", res.Request.URL, err)
	}
	return p, nil
}

func attachments(base *url.URL, p post) ([]Media, error) {
	media := []Media{}
	for _, a := range p.Data.Attachments {
		if len(media) == maxAttachments {
			break
		}
		if a.URLs.DownloadURL == "" {
			continue
		}
		u, err := resolve(base, a.URLs.DownloadURL)
		if err != nil {
			return nil, err
		}
		media = append(media, Media{URL: u.String(), Mime: a.Mime})
	}
	if len(media) == 0 {
		return nil, ErrNoMedia
	}
	return media, nil
}

func resolve(base *url.URL, raw string) (*url.URL, error) {
	u, err := base.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("anonupload: parsing %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("anonupload: %q has unsupported scheme %q", raw, u.Scheme)
	}
	return u, nil
}
