package auth

import (
	"net/http"
	"time"
)

// Cookie names, defined once here rather than as string literals scattered across every place a
// cookie is set or read — shared/auth's own Authenticate middleware reads AccessTokenCookie;
// identity/httpapi (login/refresh/logout) is the only other package that ever touches these, to SET
// them, since issuing/revoking a session is identity's job, not shared/auth's.
const (
	AccessTokenCookie  = "access_token"
	RefreshTokenCookie = "refresh_token"
)

// SetAuthCookies writes both session cookies onto w — the one place in the backend that decides the
// actual cookie flags, so every login/refresh code path gets them identically.
// specs/global/02_SECURITY_BASELINE.md §2: httpOnly (client-side JS, even from a successful XSS,
// cannot read the token — the whole point of a cookie over localStorage here), SameSite=Strict (the
// browser never attaches these cookies to a cross-site request at all, which is most of CSRF's
// mitigation for free), and Secure gated on secure (true in staging/production, which serve over
// HTTPS; false for plain-HTTP local dev, since a browser silently drops a Secure cookie over HTTP —
// hardcoding true would make local login simply not work).
func SetAuthCookies(w http.ResponseWriter, accessToken string, accessTTL time.Duration, refreshToken string, refreshTTL time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     AccessTokenCookie,
		Value:    accessToken,
		Path:     "/",
		MaxAge:   int(accessTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshTokenCookie,
		Value:    refreshToken,
		Path:     "/",
		MaxAge:   int(refreshTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearAuthCookies overwrites both cookies with an immediately-expired, empty one — what logout
// calls, so the BROWSER forgets the token even though revocation (marking refresh_token.revoked_at)
// is what actually makes the token itself stop working server-side. Clearing the cookie without also
// revoking server-side would only log the browser out, not the token — REQ-2.5 needs both.
func ClearAuthCookies(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: AccessTokenCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: RefreshTokenCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode,
	})
}
