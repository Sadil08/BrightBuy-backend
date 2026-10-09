package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// GenerateRefreshToken returns a new, cryptographically random opaque token (256 bits of entropy,
// base64url-encoded) plus its SHA-256 hash (hex-encoded). The hash is the ONLY form ever persisted
// (refresh_token.token_hash, SEC-AUTH-2: "a leaked database dump doesn't hand out usable tokens
// directly") — raw is returned to the caller exactly once, to set as a cookie, and this backend never
// stores it anywhere; it can't be recovered later even by this code, only compared against on the
// next request via HashRefreshToken.
//
// Unlike the access token (a JWT, self-describing and verifiable offline via its signature), a
// refresh token is a plain random string with no structure at all — its only job is to be looked up
// by hash in refresh_token, so a database row can be revoked (REQ-2.5's "log out everywhere"). A JWT
// can't be revoked this way short of maintaining a blocklist, which is just reinventing this table.
func GenerateRefreshToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken hashes raw the same way GenerateRefreshToken's own return value was computed —
// called again on every /auth/refresh request to compare the presented cookie's hash against what's
// stored, without this backend ever needing to hold onto (or compare against) the raw value.
func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// GenerateRandomPassword returns a random password suitable for printing once to an operator's
// terminal — used by cmd/api's --create-first-admin command (plan.md §2.2), which has no form to
// collect a chosen password from (it's a one-off CLI invocation, not an HTTP request). 20 bytes of
// entropy, base64url-encoded, comfortably clears FR-AUTH-7's 8-character minimum.
func GenerateRandomPassword() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: generate random password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
