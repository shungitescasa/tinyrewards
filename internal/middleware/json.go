package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"

	"github.com/gofiber/fiber/v3"
)

type envelope struct {
	Message *string         `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func JSON() fiber.Handler {
	return func(c fiber.Ctx) error {
		if err := c.Next(); err != nil {
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
