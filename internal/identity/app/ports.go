// Package app is identity's use-case layer: AuthService (self-serve register/login/refresh/logout)
// and AccountService (admin-only account/role/permission management). Both depend only on the
// interfaces below (the ports) plus shared/auth for the actual crypto/token mechanics — never on
// mysql directly, same dependency rule as catalog's app package.
package app

import (
	"context"
	"time"

	"brightbuy-backend/internal/identity/domain"
)

// UserRepository is everything either service needs to know about accounts.
type UserRepository interface {
	// CreateCustomer creates a user_account (role CUSTOMER) and its customer profile row together,
	// as one unit — a customer can't exist without its account or vice versa, so the adapter does
	// this inside a single transaction (shared/dbx.WithTx), not two separate calls a caller could
	// interleave something else between.
	CreateCustomer(ctx context.Context, email, passwordHash, name, phone string) (*domain.Account, error)
	// CreateStaffAccount creates a user_account under the given role plus its staff_profile row —
	// the admin-only equivalent of CreateCustomer, for every non-CUSTOMER role (FR-AUTH-9).
	CreateStaffAccount(ctx context.Context, email, passwordHash, name string, roleID int) (*domain.Account, error)
	FindByEmail(ctx context.Context, email string) (*domain.Account, error)
	FindByID(ctx context.Context, id int) (*domain.Account, error)
	ListAccounts(ctx context.Context, page, pageSize int) ([]domain.Account, int, error)
	// CountByRoleName exists for exactly one caller: cmd/api's --create-first-admin command, which
	// must refuse to run if any ADMIN account already exists (plan.md §2.2) — a narrow, specific
	// query rather than fetching every admin just to check len() > 0.
	CountByRoleName(ctx context.Context, roleName string) (int, error)
}

// RoleRepository is everything either service needs about roles/permissions. ResolvePermissionCodes
// is deliberately the raw, un-bypassed query — the ADMIN short-circuit (SEC-AUTH-1) lives in THIS
// package's resolvePermissions function (auth_service.go), one layer up, specifically so a unit test
// can prove the bypass never even calls this method for that role (a fake that panics if called).
type RoleRepository interface {
	FindByID(ctx context.Context, id int) (*domain.Role, error)
	// FindByName exists for one caller: cmd/api's --create-first-admin command, which knows the role
	// it wants by NAME (domain.RoleNameAdmin), not by a database ID it has no other way to learn.
	FindByName(ctx context.Context, name string) (*domain.Role, error)
	ResolvePermissionCodes(ctx context.Context, roleID int) ([]string, error)
	ListRolesWithPermissions(ctx context.Context) ([]domain.RoleWithPermissions, error)
	ListPermissions(ctx context.Context) ([]domain.Permission, error)
	// SetRolePermissions replaces the FULL set of permissions granted to roleID with exactly
	// permissionCodes (RolePermissionsUpdateRequest's own doc: "the full replacement set") —
	// returns ErrUnknownPermissionCode if any code isn't a real row in permission.
	SetRolePermissions(ctx context.Context, roleID int, permissionCodes []string, grantedByUserID int) (*domain.RoleWithPermissions, error)
}

// RefreshTokenRepository is everything either service needs about refresh_token rows. Every method
// here works with a HASH, never a raw token — the raw value exists only transiently, between
// shared/auth.GenerateRefreshToken and the cookie it gets written into; nothing in this interface
// could even accept one if it wanted to.
type RefreshTokenRepository interface {
	Create(ctx context.Context, userID int, tokenHash string, expiresAt time.Time) error
	FindByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error)
	Revoke(ctx context.Context, refreshTokenID int) error
	RevokeAllForUser(ctx context.Context, userID int) error
}
