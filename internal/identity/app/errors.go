package app

import "errors"

var (
	// ErrEmailAlreadyRegistered is what a UserRepository returns when a create hits the unique email
	// index — translated from whatever MySQL-specific duplicate-key error the adapter actually sees
	// (same "sentinel declared at the port level" pattern as catalog's app.ErrNotFound), so httpapi
	// can map it to 409 without ever importing mysql.
	ErrEmailAlreadyRegistered = errors.New("app: email already registered")

	// ErrInvalidCredentials is the ONE error Login ever returns for a failed attempt — unknown
	// email, wrong password, and a deactivated account all return exactly this (FR-AUTH-8: "the same
	// generic message regardless of whether the email exists").
	ErrInvalidCredentials = errors.New("app: invalid credentials")

	// ErrUnknownRole: a roleId that doesn't exist in the role table — plan.md §6: "400, not 500."
	ErrUnknownRole = errors.New("app: unknown role")

	// ErrUnknownPermissionCode: PUT .../permissions supplied a code the permission table has never
	// heard of — plan.md §6's other named 400 case.
	ErrUnknownPermissionCode = errors.New("app: unknown permission code")

	// ErrRefreshTokenInvalid covers every way a refresh attempt can fail — unknown token, expired,
	// already revoked/reused. One sentinel, not several, for the same no-oracle reason
	// ErrInvalidCredentials is one message: a caller learning WHY their refresh failed learns
	// something an attacker could use to distinguish token states.
	ErrRefreshTokenInvalid = errors.New("app: refresh token invalid")
)
