// Package auth is the shared MECHANISM every module's httpapi package depends on: hashing/verifying
// passwords, issuing/verifying JWTs, and the middleware that turns a request's cookie into an
// authenticated identity in context. It deliberately does NOT live in internal/identity — identity
// owns the DATA (accounts, roles, permissions); every other module (catalog, cart, ordering, ...)
// needs RequirePermission/RequireRole without needing to import identity's internals, which is
// exactly the dependency identity-as-owner would force if this lived there instead
// (specs/global/06_ENGINEERING_STANDARDS.md §4, plan.md §1).
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is deliberately a constant, not configurable — specs/global/02_SECURITY_BASELINE.md §3
// requires "cost factor >= 12" as a fixed floor, not a tunable knob a future PR could quietly lower.
const bcryptCost = 12

// HashPassword returns a BCrypt hash of plaintext, safe to store in user_account.password_hash.
// BCrypt already embeds a random salt per call — two calls with the same plaintext produce different
// hashes, which is why there's no separate "salt" field anywhere in the schema.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// ComparePassword reports whether plaintext is the password that produced hash. Returns false for
// any error (malformed hash, mismatch) — callers never need to distinguish "wrong password" from
// "hash was garbage," both just mean "this login attempt fails" (FR-AUTH-8's generic failure).
func ComparePassword(hash, plaintext string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext)) == nil
}
