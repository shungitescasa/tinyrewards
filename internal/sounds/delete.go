package sounds

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
)

func (h *Handler) Delete(c fiber.Ctx) error {
	if err := h.opts.Authenticate(c); err != nil {
		return err
	}
	if err := h.Remove(c.Context(), c.FormValue("id"), account.UserID(c)); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Handler) Remove(ctx context.Context, uploadID, userID string) error {
	if uploadID == "" {
		return fiber.NewError(fiber.StatusBadRequest, `missing "id" field`)
	}
	if userID == "" {
		return fiber.NewError(fiber.StatusUnauthorized, "authentication is required to delete sounds")
	}
	err := h.remove(ctx, uploadID, userID)
	var fe *fiber.Error
	if err != nil && !errors.As(err, &fe) {
		log.Printf("sounds: deleting upload %s: %v", uploadID, err)
		return fiber.ErrInternalServerError
	}
	return err
}

func (h *Handler) remove(ctx context.Context, uploadID, userID string) error {
	tx, err := h.opts.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var owner sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT user_id FROM uploads WHERE id = ? FOR UPDATE`, uploadID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "upload not found")
	}
	if err != nil {
		return err
	}
	if !owner.Valid || owner.String != userID {
		return fiber.NewError(fiber.StatusForbidden, "you can only delete your own sounds")
	}

	fileIDs, err := uploadFileIDs(ctx, tx, uploadID)
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM uploads WHERE id = ?`, uploadID); err != nil {
		return err
	}

	for _, fileID := range fileIDs {
		var extension string
		err := tx.QueryRowContext(ctx, `SELECT extension FROM files WHERE id = ? FOR UPDATE`, fileID).Scan(&extension)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		var users int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM upload_files WHERE file_id = ? FOR UPDATE`, fileID,
		).Scan(&users); err != nil {
			return err
		}
		if users > 0 {
			continue
		}

		if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE id = ?`, fileID); err != nil {
			return err
		}
		if err := h.opts.Storage.Delete(ctx, fileID+"."+extension); err != nil {
			return fmt.Errorf("deleting file %s: %w", fileID, err)
		}
	}

	return tx.Commit()
}

func uploadFileIDs(ctx context.Context, tx *sql.Tx, uploadID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT file_id FROM upload_files WHERE upload_id = ?`, uploadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
