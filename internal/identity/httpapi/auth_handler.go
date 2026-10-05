package httpapi

import (
	"errors"
	"net/http"

	"brightbuy-backend/internal/identity/app"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"
	"brightbuy-backend/internal/shared/ratelimit"
)

// AuthHandler is the self-serve half: register/login/refresh/logout/me. loginEmailLimiter is held
// here (not just wired as route middleware like the IP-based limiters) because its key — the
// SUBMITTED email — only exists after the request body has been decoded, which happens inside this
// handler, not before it (ratelimit.Limiter's own doc explains this split).
type AuthHandler struct {
	auth              *app.AuthService
	cookieSecure      bool
	loginEmailLimiter *ratelimit.Limiter
}

func NewAuthHandler(authService *app.AuthService, cookieSecure bool, loginEmailLimiter *ratelimit.Limiter) *AuthHandler {
	return &AuthHandler{auth: authService, cookieSecure: cookieSecure, loginEmailLimiter: loginEmailLimiter}
}

// Register is FR-AUTH-1. Always creates a CUSTOMER — there's no role field anywhere on
// RegisterRequestDTO for a visitor to submit, which is what actually prevents self-service
// escalation (not a check that rejects a role field, the simpler fact that the field doesn't exist).
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	req, err := httpx.DecodeAndValidate[RegisterRequestDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}

	acct, err := h.auth.Register(r.Context(), app.RegisterInput{
		Name: req.Name, Email: req.Email, Password: req.Password, Phone: req.Phone,
	})
	if err != nil {
		if errors.Is(err, app.ErrEmailAlreadyRegistered) {
			httpx.WriteError(w, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "an account with this email already exists")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toUserProfileDTO(*acct))
}

// Login is FR-AUTH-8: one generic 401 for every failure reason, by construction — AuthService.Login
// already collapsed "unknown email"/"wrong password"/"deactivated" into the single
// app.ErrInvalidCredentials this handler checks for, so there's no way for a second branch to
// accidentally leak a more specific message here even by a future careless edit.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	req, err := httpx.DecodeAndValidate[LoginRequestDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}

	// Per-email limiter, checked here rather than as route middleware: SEC-AUTH-3's "rate-limited
	// per IP AND per email" needs the email, which only exists once the body above is decoded.
	if !h.loginEmailLimiter.Allow(req.Email) {
		httpx.WriteError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, try again later")
		return
	}

	tokens, err := h.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	auth.SetAuthCookies(w, tokens.AccessToken, tokens.AccessTokenTTL, tokens.RefreshToken, tokens.RefreshTokenTTL, h.cookieSecure)
	httpx.WriteJSON(w, http.StatusOK, toUserProfileDTO(*tokens.Account))
}

// Refresh reads the refresh cookie directly (no request body at all, per openapi.yaml) — this route
// deliberately does NOT sit behind auth.Authenticate middleware, since the whole point of refresh is
// getting a NEW access token once the old one has expired; requiring a still-valid access token to
// call it would defeat the endpoint's purpose.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.RefreshTokenCookie)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "refresh token required")
		return
	}

	tokens, err := h.auth.Refresh(r.Context(), cookie.Value)
	if err != nil {
		httpx.WriteError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "refresh token invalid or expired")
		return
	}

	auth.SetAuthCookies(w, tokens.AccessToken, tokens.AccessTokenTTL, tokens.RefreshToken, tokens.RefreshTokenTTL, h.cookieSecure)
	httpx.WriteJSON(w, http.StatusOK, toUserProfileDTO(*tokens.Account))
}

// Logout is FR-AUTH-5. Sits behind auth.Authenticate (RegisterRoutes), so an access token is
// required to call it at all — but the actual revocation targets the REFRESH cookie, since that's
// the thing a subsequent /auth/refresh would otherwise still accept. Missing/already-invalid refresh
// cookie is not an error here: the caller is logged out either way once this returns.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.RefreshTokenCookie); err == nil {
		_ = h.auth.Logout(r.Context(), cookie.Value)
	}
	auth.ClearAuthCookies(w, h.cookieSecure)
	w.WriteHeader(http.StatusNoContent)
}

// Me is GET /auth/me — sits behind auth.Authenticate, so ClaimsFromContext is guaranteed non-nil
// here (RegisterRoutes never wires this route without that middleware first).
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}

	acct, err := h.auth.GetAccount(r.Context(), claims.UserID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toUserProfileDTO(*acct))
}
