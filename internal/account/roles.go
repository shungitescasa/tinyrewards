package account

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

const (
	RoleAssign = "assign"
	RoleRevoke = "revoke"
)

type Permissions struct {
	ModerateSounds bool `json:"permission_moderate_sounds"`
	ManageRoles    bool `json:"permission_manage_roles"`
}

const permissionColumns = `r.permission_moderate_sounds, r.permission_manage_roles`

func (p *Permissions) fields() []any {
	return []any{&p.ModerateSounds, &p.ManageRoles}
}

func (p Permissions) merge(o Permissions) Permissions {
	return Permissions{
		ModerateSounds: p.ModerateSounds || o.ModerateSounds,
		ManageRoles:    p.ManageRoles || o.ManageRoles,
	}
}

type Role struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Icon        string      `json:"icon"`
	Priority    int         `json:"priority"`
	Permissions Permissions `json:"permissions"`
}

type Assignment struct {
	UserID     string    `json:"user_id"`
	Username   string    `json:"username"`
	Role       Role      `json:"role"`
	AssignedAt time.Time `json:"assigned_at"`
}

const selectRole = `SELECT r.id, r.name, COALESCE(r.icon, ''), r.priority, ` + permissionColumns

func scanRole(scan func(...any) error, extra ...any) (Role, error) {
	var r Role
	dest := append([]any{&r.ID, &r.Name, &r.Icon, &r.Priority}, r.Permissions.fields()...)
	err := scan(append(dest, extra...)...)
	return r, err
}

func (h *Handler) userRoles(ctx context.Context, userID string) ([]Role, error) {
	rows, err := h.db.QueryContext(ctx,
		selectRole+` FROM role_assigns ra
		 JOIN roles r ON r.id = ra.role_id
		 WHERE ra.user_id = ?
		 ORDER BY r.priority DESC, r.id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := []Role{}
	for rows.Next() {
		r, err := scanRole(rows.Scan)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

func (h *Handler) Roles(ctx context.Context) ([]Role, error) {
	rows, err := h.db.QueryContext(ctx, selectRole+` FROM roles r ORDER BY r.priority DESC, r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := []Role{}
	for rows.Next() {
		r, err := scanRole(rows.Scan)
		if err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

func (h *Handler) Assignments(ctx context.Context) ([]Assignment, error) {
	rows, err := h.db.QueryContext(ctx,
		selectRole+`, u.id, u.username, ra.assigned_at
		 FROM role_assigns ra
		 JOIN roles r ON r.id = ra.role_id
		 JOIN users u ON u.id = ra.user_id
		 WHERE ra.role_id <> ?
		 ORDER BY r.priority DESC, r.id, u.username`,
		DefaultRole,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []Assignment{}
	for rows.Next() {
		var a Assignment
		a.Role, err = scanRole(rows.Scan, &a.UserID, &a.Username, &a.AssignedAt)
		if err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (h *Handler) ChangeRole(ctx context.Context, actorID, action, user, roleID string) error {
	user, roleID = strings.TrimSpace(user), strings.TrimSpace(roleID)
	switch {
	case user == "":
		return fiber.NewError(fiber.StatusBadRequest, `missing "user" field`)
	case roleID == "":
		return fiber.NewError(fiber.StatusBadRequest, `missing "role" field`)
	case roleID == DefaultRole:
		return fiber.NewError(fiber.StatusBadRequest, "the default role is given to everyone and cannot be changed")
	}

	var err error
	switch action {
	case RoleAssign:
		err = h.assignRole(ctx, user, roleID)
	case RoleRevoke:
		err = h.revokeRole(ctx, actorID, user, roleID)
	default:
		return fiber.NewError(fiber.StatusBadRequest, `action must be "assign" or "revoke"`)
	}

	var fe *fiber.Error
	if err != nil && !errors.As(err, &fe) {
		log.Printf("account: %s role %s for %s: %v", action, roleID, user, err)
		return fiber.ErrInternalServerError
	}
	return err
}

func (h *Handler) assignRole(ctx context.Context, user, roleID string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	userID, err := lookupUser(ctx, tx, user)
	if err != nil {
		return err
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM roles WHERE id = ?`, roleID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fiber.NewError(fiber.StatusNotFound, "role not found")
	}
	if err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, `INSERT IGNORE INTO role_assigns (role_id, user_id) VALUES (?, ?)`, roleID, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fiber.NewError(fiber.StatusConflict, "the user already has this role")
	}
	return tx.Commit()
}

func (h *Handler) revokeRole(ctx context.Context, actorID, user, roleID string) error {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	locked, err := tx.QueryContext(ctx, `SELECT id FROM roles WHERE permission_manage_roles FOR UPDATE`)
	if err != nil {
		return err
	}
	if err := locked.Close(); err != nil {
		return err
	}

	userID, err := lookupUser(ctx, tx, user)
	if err != nil {
		return err
	}
	if userID == actorID {
		return fiber.NewError(fiber.StatusForbidden, "you cannot revoke your own roles")
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM role_assigns WHERE role_id = ? AND user_id = ?`, roleID, userID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return fiber.NewError(fiber.StatusNotFound, "the user does not have this role")
	}

	var managers int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM role_assigns ra
		 JOIN roles r ON r.id = ra.role_id
		 WHERE r.permission_manage_roles`,
	).Scan(&managers); err != nil {
		return err
	}
	if managers == 0 {
		return fiber.NewError(fiber.StatusConflict, "at least one user must be able to manage roles")
	}
	return tx.Commit()
}

func lookupUser(ctx context.Context, tx *sql.Tx, user string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM users WHERE id = ? OR username = ? ORDER BY id = ? DESC LIMIT 1`,
		user, user, user,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fiber.NewError(fiber.StatusNotFound, "user not found")
	}
	return id, err
}
