package rewards

import (
	"fmt"
	"log"
	"time"

	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"
)

const (
	eventsKeepalive    = 30 * time.Second
	eventsWriteTimeout = 10 * time.Second
	eventsReadLimit    = 512
)

var keepaliveMessage = []byte(`{"type":"keepalive"}`)

func (h *Handler) eventsUpgrade(c fiber.Ctx) error {
	if h.opts.EventSub == nil {
		return fiber.NewError(fiber.StatusNotImplemented, ErrTwitchDisabled.Error())
	}
	if !websocket.IsWebSocketUpgrade(c) {
		return fiber.ErrUpgradeRequired
	}
	userID := c.Query("user_id")
	if userID == "" {
		return fiber.NewError(fiber.StatusBadRequest, `missing "user_id" query parameter`)
	}
	exists, err := h.userExists(c.Context(), userID)
	if err != nil {
		log.Printf("rewards: checking user: %v", err)
		return fiber.ErrInternalServerError
	}
	if !exists {
		return fiber.NewError(fiber.StatusNotFound, fmt.Sprintf("user %q not found", userID))
	}
	return c.Next()
}

func (h *Handler) Events(conn *websocket.Conn) {
	defer conn.Close()

	listener, err := h.opts.EventSub.Listen(conn.Query("user_id"))
	if err != nil {
		conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, err.Error()),
			time.Now().Add(eventsWriteTimeout))
		return
	}
	defer listener.Close()

	closed := make(chan struct{})
	go func() {
		defer close(closed)
		conn.SetReadLimit(eventsReadLimit)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(eventsKeepalive)
	defer ticker.Stop()

	for {
		var data []byte
		select {
		case <-closed:
			return
		case data = <-listener.C:
		case <-ticker.C:
			data = keepaliveMessage
		}
		conn.SetWriteDeadline(time.Now().Add(eventsWriteTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}
	}
}
