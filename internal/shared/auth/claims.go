package auth

import "context"

// Claims is what every valid access token carries, and the ONLY place the rest of the backend is
// allowed to learn who's making a request and what they can do — specs/global/02_SECURITY_BASELINE.md
// §2: "the acting user is resolved ONLY from a verified JWT's subject claim... any customer_id/
// user_id/role field present in a request body is a validation error, not a hint."
//
// Permissions carries the account's RESOLVED permission list at issue/refresh time (or the single
// sentinel "*" for ADMIN — see identity/app's RoleService) rather than a role name alone, so
// shared/auth's middleware never needs to know ANYTHING about role_permission or query the database
// per request (02_SECURITY_BASELINE.md §1.1) — it just checks "is this code in the list," which is
// why this package can sit below identity without depending on it.
type Claims struct {
	UserID      int      `json:"sub"`
	Role        string   `json:"role"`
	Permissions []string `json:"perms"`
}

// HasPermission reports whether these claims grant code — true if code is explicitly present, or if
// the wildcard "*" is (the ADMIN bypass, SEC-AUTH-1: never a per-permission check for that role, one
// sentinel value that matches everything).
func (c Claims) HasPermission(code string) bool {
	for _, p := range c.Permissions {
		if p == "*" || p == code {
			return true
		}
	}
	return false
}

// contextKey is an unexported type specifically so a context key collision with another package is
// structurally impossible — even if another package also used a plain string "claims" as its context
// key, Go's context.Value comparison includes the key's TYPE, not just its value, so the two can
// never be confused for each other. This is the standard idiom for anything stored in a
// context.Context, recommended directly by the context package's own documentation.
type contextKey int

const claimsKey contextKey = 0

// ContextWithClaims returns a copy of ctx carrying claims — called once, by Authenticate middleware,
// right after a request's token has been verified.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext retrieves whatever Claims Authenticate middleware already verified and stored for
// this request — nil if there are none (an unauthenticated request never reached Authenticate, or
// this route doesn't require it). Every handler/middleware downstream of Authenticate calls this
// instead of re-parsing a cookie or token itself.
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsKey).(*Claims)
	return claims
}
