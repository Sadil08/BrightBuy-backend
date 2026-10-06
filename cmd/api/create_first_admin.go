package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	identitydomain "brightbuy-backend/internal/identity/domain"
	identitymysql "brightbuy-backend/internal/identity/mysql"
	"brightbuy-backend/internal/shared/auth"
)

// createFirstAdmin implements plan.md §2.2: a one-off CLI path (`go run ./cmd/api
// --create-first-admin`) for creating the very first ADMIN account in a real deployment, where
// there's no existing admin yet to authorize the normal POST /admin/users flow — the one case this
// whole module's design assumes can't happen, has to happen exactly once, outside the API entirely.
//
// Refuses to run at all if any ADMIN account already exists, so this can never become a second way
// in alongside legitimate admins — it's a bootstrap mechanism, not an admin-creation endpoint with
// extra steps.
func createFirstAdmin(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	users := identitymysql.NewUserRepository(db)
	roles := identitymysql.NewRoleRepository(db)

	existingAdmins, err := users.CountByRoleName(ctx, identitydomain.RoleNameAdmin)
	if err != nil {
		return fmt.Errorf("check existing admin accounts: %w", err)
	}
	if existingAdmins > 0 {
		return fmt.Errorf("refusing to run: %d ADMIN account(s) already exist", existingAdmins)
	}

	adminRole, err := roles.FindByName(ctx, identitydomain.RoleNameAdmin)
	if err != nil {
		return fmt.Errorf("find ADMIN role (has migration 0004 run?): %w", err)
	}

	// A random, never-chosen password: this is a CLI invocation with no form to collect one from,
	// and the operator running it is trusted to immediately change it after first login (there's no
	// "generate a weak default" temptation here, since this one is already strong by construction).
	password, err := auth.GenerateRandomPassword()
	if err != nil {
		return fmt.Errorf("generate password: %w", err)
	}
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	const email = "admin@brightbuy.local"
	account, err := users.CreateStaffAccount(ctx, email, passwordHash, "First Administrator", adminRole.ID)
	if err != nil {
		return fmt.Errorf("create admin account: %w", err)
	}

	logger.Info("first admin account created", "userAccountId", account.ID, "email", email)

	// Printed to stdout, not logged via slog — this is a one-time operator handoff, not a log line
	// that might end up aggregated/retained somewhere passwords shouldn't persist (FR-AUTH-3's
	// "never logged" extends naturally to this too, even though it's a system-generated password,
	// not a user's own).
	fmt.Println("=========================================================")
	fmt.Println("First ADMIN account created. SAVE THIS PASSWORD NOW:")
	fmt.Println("  email:   ", email)
	fmt.Println("  password:", password)
	fmt.Println("This password will never be shown or stored anywhere again.")
	fmt.Println("=========================================================")

	return nil
}
