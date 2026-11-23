package web

import (
	"github.com/gofiber/fiber/v3"

	"git.shungites.casa/tiny/rewards/internal/account"
)

var roleNotices = map[string]string{
	account.RoleAssign: "Role assigned.",
	account.RoleRevoke: "Role revoked.",
}

func (h *Handler) rolesPage(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	if !v.RoleManager() {
		return fiber.NewError(fiber.StatusForbidden, "Only admins can see this page.")
	}

	ctx := c.Context()
	roles, err := h.opts.Accounts.Roles(ctx)
	if err != nil {
		return err
	}
	assignments, err := h.opts.Accounts.Assignments(ctx)
	if err != nil {
		return err
	}
	return h.render(c, fiber.StatusOK, "roles", fiber.Map{
		"Roles":       roles,
		"Assignments": assignments,
		"DefaultRole": account.DefaultRole,
		"User":        c.Query("user"),
	})
}

func (h *Handler) rolesJSON(c fiber.Ctx) error {
	if _, err := h.opts.Accounts.RequireRoleManager(c); err != nil {
		return err
	}
	ctx := c.Context()
	roles, err := h.opts.Accounts.Roles(ctx)
	if err != nil {
		return err
	}
	assignments, err := h.opts.Accounts.Assignments(ctx)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"roles": roles, "assignments": assignments})
}

func (h *Handler) changeRole(c fiber.Ctx) error {
	v := h.viewer(c)
	if v == nil {
		return toLogin(c)
	}
	if !v.RoleManager() {
		return fiber.NewError(fiber.StatusForbidden, "Only admins can do this.")
	}

	action := c.FormValue("action")
	err := h.opts.Accounts.ChangeRole(c.Context(), v.ID, action, c.FormValue("user"), c.FormValue("role"))
	if _, message, ok := userError(err); ok {
		return c.Redirect().With("error", message).To("/system/roles")
	}
	if err != nil {
		return err
	}
	return c.Redirect().With("success", roleNotices[action]).To("/system/roles")
}

func (h *Handler) changeRoleJSON(c fiber.Ctx) error {
	u, err := h.opts.Accounts.RequireRoleManager(c)
	if err != nil {
		return err
	}
	if err := h.opts.Accounts.ChangeRole(c.Context(), u.ID, c.FormValue("action"), c.FormValue("user"), c.FormValue("role")); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
