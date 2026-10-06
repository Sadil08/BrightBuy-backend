package auth

import (
	"net/http"

	"brightbuy-backend/internal/shared/httpx"
)

// Authenticate reads the access token cookie, verifies it, and — if valid — injects the resulting
// Claims into the request context (ClaimsFromContext) before calling the next handler. Missing,
// expired, or otherwise invalid -> 401, and next never runs at all
// (specs/global/02_SECURITY_BASELINE.md §2: "expired/invalid -> 401").
//
// This is the ONLY place in the entire backend that reads AccessTokenCookie or calls
// VerifyAccessToken — every other piece of code, in every module, learns who's calling purely by
// reading ClaimsFromContext(r.Context()) downstream of this middleware. That's what makes "the
// acting user is resolved ONLY from a verified JWT" (02 §2) actually true rather than aspirational:
// there's exactly one code path that could get it wrong, not one per handler.
func Authenticate(issuer *TokenIssuer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(AccessTokenCookie)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
				return
			}

			claims, err := issuer.VerifyAccessToken(cookie.Value)
			if err != nil {
				httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
				return
			}

			next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
		})
	}
}

// RequirePermission builds middleware that 403s any request whose claims don't grant code (via
// Claims.HasPermission — true for an exact match, or ADMIN's "*" wildcard, SEC-AUTH-1). Must run
// AFTER Authenticate in the middleware chain — it only reads claims already placed in context, it
// never verifies a token itself. A request with no claims at all (Authenticate never ran, or the
// token was invalid and Authenticate already 401'd) is treated as "definitely doesn't have this
// permission," not a crash — nil-safe by construction, since the zero value of *Claims is nil and
// HasPermission is called on a dereferenced copy only after that nil check.
func RequirePermission(code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil || !claims.HasPermission(code) {
				httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "missing permission: "+code)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireRole builds middleware that 403s any request whose claims' Role isn't one of allowed —
// for the rare case a route is gated by role identity rather than a specific permission (e.g. "must
// be a CUSTOMER," which isn't a capability in the permission table at all — specs/global/
// 02_SECURITY_BASELINE.md §1: "customer actions are ownership-scoped, not permission-scoped").
func RequireRole(allowed ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := ClaimsFromContext(r.Context())
			if claims == nil {
				httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "insufficient role")
				return
			}
			for _, role := range allowed {
				if claims.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "insufficient role")
		})
	}
}
