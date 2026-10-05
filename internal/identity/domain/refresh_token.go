package domain

import "time"

// RefreshToken mirrors one refresh_token row. TokenHash is the SHA-256 hash shared/auth computes
// (SEC-AUTH-2) — this type never carries the raw token value, because nothing downstream of the
// repository is ever supposed to have it; the raw value exists only transiently, between
// shared/auth.GenerateRefreshToken and the cookie it gets written into.
type RefreshToken struct {
	ID        int
	UserID    int
	TokenHash string
	ExpiresAt time.Time
	// RevokedAt is nil for a token that's still valid — set to a real time once logout (or
	// reused-token-detection, plan.md §6) revokes it. A pointer, not a bool, so "revoked" also
	// records WHEN, for the audit trail 02_SECURITY_BASELINE.md §7 asks for.
	RevokedAt *time.Time
}

// IsValid reports whether this token can still be used to refresh a session: not expired, not
// revoked. Centralizing this check here (rather than repeating "revokedAt == nil &&
// expiresAt.After(now)" at every call site) means there's exactly one place that could get the
// expiry/revocation logic wrong, not one per caller.
func (t RefreshToken) IsValid(now time.Time) bool {
	return t.RevokedAt == nil && t.ExpiresAt.After(now)
}
