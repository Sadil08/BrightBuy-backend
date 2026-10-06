package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueAndVerifyAccessTokenRoundTrip(t *testing.T) {
	issuer := NewTokenIssuer("test-signing-key")

	token, err := issuer.IssueAccessToken(42, "WAREHOUSE_STAFF", []string{"catalog:write", "stock:adjust"})
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := issuer.VerifyAccessToken(token)
	if err != nil {
		t.Fatalf("VerifyAccessToken: %v", err)
	}

	if claims.UserID != 42 {
		t.Errorf("UserID = %d, want 42", claims.UserID)
	}
	if claims.Role != "WAREHOUSE_STAFF" {
		t.Errorf("Role = %q, want WAREHOUSE_STAFF", claims.Role)
	}
	if len(claims.Permissions) != 2 || claims.Permissions[0] != "catalog:write" {
		t.Errorf("Permissions = %v, want [catalog:write stock:adjust]", claims.Permissions)
	}
}

func TestVerifyAccessTokenRejectsWrongSigningKey(t *testing.T) {
	issuedBy := NewTokenIssuer("key-one")
	verifiedBy := NewTokenIssuer("key-two")

	token, err := issuedBy.IssueAccessToken(1, "CUSTOMER", nil)
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	if _, err := verifiedBy.VerifyAccessToken(token); err == nil {
		t.Fatal("VerifyAccessToken with the wrong key returned no error, want ErrInvalidToken")
	}
}

func TestVerifyAccessTokenRejectsExpiredToken(t *testing.T) {
	issuer := NewTokenIssuer("test-signing-key")

	// Hand-build an already-expired token rather than issuing a real one and sleeping 15 minutes —
	// same jwtClaims shape IssueAccessToken produces, just with ExpiresAt set in the past.
	claims := jwtClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "7",
			Issuer:    "brightbuy",
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Minute)),
		},
		Role: "CUSTOMER",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(issuer.signingKey)
	if err != nil {
		t.Fatalf("sign expired token: %v", err)
	}

	if _, err := issuer.VerifyAccessToken(signed); err == nil {
		t.Fatal("VerifyAccessToken on an expired token returned no error, want ErrInvalidToken")
	}
}

func TestVerifyAccessTokenRejectsGarbage(t *testing.T) {
	issuer := NewTokenIssuer("test-signing-key")
	if _, err := issuer.VerifyAccessToken("not-a-real-token"); err == nil {
		t.Fatal("VerifyAccessToken on garbage input returned no error, want ErrInvalidToken")
	}
}

func TestClaimsHasPermission(t *testing.T) {
	tests := []struct {
		name  string
		perms []string
		code  string
		want  bool
	}{
		{"exact match", []string{"catalog:write"}, "catalog:write", true},
		{"no match", []string{"catalog:write"}, "stock:adjust", false},
		{"ADMIN wildcard matches anything", []string{"*"}, "account:manage_roles", true},
		{"empty permissions", nil, "catalog:write", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Claims{Permissions: tt.perms}
			if got := c.HasPermission(tt.code); got != tt.want {
				t.Errorf("HasPermission(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

func TestCustomerIDRoundTripsThroughToken(t *testing.T) {
	issuer := NewTokenIssuer("test-signing-key-test-signing-key-123")
	token, err := issuer.IssueAccessTokenForCustomer(3, 9, "CUSTOMER", nil)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := issuer.VerifyAccessToken(token)
	if err != nil || claims.UserID != 3 || claims.CustomerID != 9 {
		t.Fatalf("got %+v, %v; want UserID 3, CustomerID 9", claims, err)
	}
	plain, _ := issuer.IssueAccessToken(3, "ADMIN", nil)
	claims, _ = issuer.VerifyAccessToken(plain)
	if claims.CustomerID != 0 {
		t.Fatalf("CustomerID = %d, want 0 when none was set", claims.CustomerID)
	}
}
