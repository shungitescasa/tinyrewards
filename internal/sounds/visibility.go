package sounds

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v3"
)

const (
	VisibilityUnlisted = -1
	VisibilityPending  = 0
	VisibilityPublic   = 1
)

const (
	ModeratePublish = "publish"
	ModerateUnlist  = "unlist"
	ModerateDelete  = "delete"
)

var ErrInvalidVisibility = errors.New(`visibility must be "public" or "unlisted"`)

func ParseVisibility(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "1", "public":
		return VisibilityPublic, nil
	case "-1", "unlisted":
		return VisibilityUnlisted, nil
	}
	return 0, ErrInvalidVisibility
}

func (h *Handler) ModerationEnabled() bool {
	return h.opts.Moderation
}

func (h *Handler) Moderate(ctx context.Context, uploadID, action string) error {
	if uploadID == "" {
		return fiber.NewError(fiber.StatusBadRequest, `missing "id" field`)
	}

	var err error
	switch action {
	case ModeratePublish:
		err = h.setPendingVisibility(ctx, uploadID, VisibilityPublic)
	case ModerateUnlist:
		err = h.setPendingVisibility(ctx, uploadID, VisibilityUnlisted)
	case ModerateDelete:
		err = h.remove(ctx, uploadID, "")
	default:
		return fiber.NewError(fiber.StatusBadRequest, `action must be "publish", "unlist" or "delete"`)
	}

	var fe *fiber.Error
	if err != nil && !errors.As(err, &fe) {
		log.Printf("sounds: moderating upload %s: %v", uploadID, err)
		return fiber.ErrInternalServerError
	}
	return err
}

func (h *Handler) setPendingVisibility(ctx context.Context, uploadID string, visibility int) error {
	res, err := h.opts.DB.ExecContext(ctx,
		`UPDATE uploads SET visibility = ? WHERE id = ? AND visibility = ?`,
		visibility, uploadID, VisibilityPending,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fiber.NewError(fiber.StatusNotFound, "no pending upload with this id")
	}
	return nil
}
