package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthenticateRejectsRequestWithNoCookie(t *testing.T) {
	issuer := NewTokenIssuer("test-key")
	handler := Authenticate(issuer)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticateRejectsInvalidCookie(t *testing.T) {
	issuer := NewTokenIssuer("test-key")
	handler := Authenticate(issuer)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: "garbage"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticateAcceptsValidCookieAndInjectsClaims(t *testing.T) {
	issuer := NewTokenIssuer("test-key")
	token, err := issuer.IssueAccessToken(5, "CUSTOMER", nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	var sawUserID int
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil {
			t.Fatal("ClaimsFromContext returned nil inside the handler, want the verified claims")
		}
		sawUserID = claims.UserID
		w.WriteHeader(http.StatusOK)
	})
	handler := Authenticate(issuer)(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: AccessTokenCookie, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if sawUserID != 5 {
		t.Errorf("claims.UserID inside handler = %d, want 5", sawUserID)
	}
}

func TestRequirePermissionAllowsExactMatch(t *testing.T) {
	handler := RequirePermission("stock:adjust")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Permissions: []string{"stock:adjust"}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequirePermissionAllowsAdminWildcard(t *testing.T) {
	handler := RequirePermission("account:manage_roles")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Role: "ADMIN", Permissions: []string{"*"}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (ADMIN's wildcard should pass any permission check)", rec.Code, http.StatusOK)
	}
}

func TestRequirePermissionRejectsMissingPermission(t *testing.T) {
	handler := RequirePermission("stock:adjust")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Permissions: []string{"catalog:write"}}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequirePermissionRejectsNoClaimsAtAll(t *testing.T) {
	// Simulates RequirePermission somehow running without Authenticate first — should fail closed,
	// never panic on a nil claims pointer.
	handler := RequirePermission("stock:adjust")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireRoleAllowsListedRole(t *testing.T) {
	handler := RequireRole("CUSTOMER", "ADMIN")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Role: "CUSTOMER"}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRoleRejectsUnlistedRole(t *testing.T) {
	handler := RequireRole("ADMIN")(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(ContextWithClaims(req.Context(), &Claims{Role: "CUSTOMER"}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
