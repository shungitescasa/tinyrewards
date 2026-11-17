package account

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidToken = errors.New("invalid or expired token")

var tokenHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

type tokenClaims struct {
	Subject   string `json:"sub"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type tokens struct {
	secret []byte
	ttl    time.Duration
}

func (t *tokens) issue(userID string, now time.Time) (string, time.Time, error) {
	expires := now.Add(t.ttl).Truncate(time.Second)
	payload, err := json.Marshal(tokenClaims{
		Subject:   userID,
		IssuedAt:  now.Unix(),
		ExpiresAt: expires.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	signed := tokenHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return signed + "." + base64.RawURLEncoding.EncodeToString(t.sign(signed)), expires, nil
}

func (t *tokens) verify(token string, now time.Time) (string, error) {
	header, rest, ok := strings.Cut(token, ".")
	if !ok || header != tokenHeader {
		return "", ErrInvalidToken
	}
	payload, sig, ok := strings.Cut(rest, ".")
	if !ok {
		return "", ErrInvalidToken
	}

	mac, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(mac, t.sign(header+"."+payload)) {
		return "", ErrInvalidToken
	}

	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", ErrInvalidToken
	}
	var claims tokenClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", ErrInvalidToken
	}
	if claims.Subject == "" || now.Unix() >= claims.ExpiresAt {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}

func (t *tokens) sign(data string) []byte {
	h := hmac.New(sha256.New, t.secret)
	h.Write([]byte(data))
	return h.Sum(nil)
}
