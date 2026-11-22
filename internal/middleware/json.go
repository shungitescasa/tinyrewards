package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

type envelope struct {
	Message *string         `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func WantsHTML(c fiber.Ctx) bool {
	for part := range strings.SplitSeq(c.Get(fiber.HeaderAccept), ",") {
		mediaType, params, _ := strings.Cut(part, ";")
		if !strings.EqualFold(strings.TrimSpace(mediaType), fiber.MIMETextHTML) {
			continue
		}
		for param := range strings.SplitSeq(params, ";") {
			key, value, _ := strings.Cut(param, "=")
			if !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			if q, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && q <= 0 {
				return false
			}
		}
		return true
	}
	return false
}

func Negotiate(html, api fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Append(fiber.HeaderVary, fiber.HeaderAccept)
		if WantsHTML(c) {
			return html(c)
		}
		return wrap(c, api(c))
	}
}

func wrap(c fiber.Ctx, err error) error {
	if err != nil {
		return WriteError(c, err)
	}

	res := c.Response()
	if !bytes.HasPrefix(res.Header.ContentType(), []byte(fiber.MIMEApplicationJSON)) {
		return nil
	}

	var data json.RawMessage
	if body := res.Body(); len(bytes.TrimSpace(body)) != 0 {
		data = body
	}
	out, err := json.Marshal(envelope{Data: data})
	if err != nil {
		return WriteError(c, err)
	}
	res.SetBody(out)
	return nil
}

func WriteError(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	message := fiber.ErrInternalServerError.Message

	var fe *fiber.Error
	if errors.As(err, &fe) {
		code = fe.Code
		message = fe.Message
	} else {
		log.Printf("%s %s: %v", c.Method(), c.Path(), err)
	}

	out, merr := json.Marshal(envelope{Message: &message})
	if merr != nil {
		return merr
	}
	c.Status(code)
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Send(out)
}
