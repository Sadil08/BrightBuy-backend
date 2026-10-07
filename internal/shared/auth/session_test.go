package auth

import (
	"testing"
	"time"
)

func TestCustomerTokenIssueAndVerify(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	token, err := IssueCustomerToken(key, 12, 34, now)
	if err != nil {
		t.Fatalf("IssueCustomerToken: %v", err)
	}
	claims, err := VerifyCustomerToken(token, key, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("VerifyCustomerToken: %v", err)
	}
	if claims.UserID != 12 || claims.CustomerID != 34 || claims.Role != "CUSTOMER" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestCustomerTokenRejectsTamperingAndExpiration(t *testing.T) {
	now := time.Now()
	key := []byte("0123456789abcdef0123456789abcdef")
	token, err := IssueCustomerToken(key, 12, 34, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCustomerToken(token+"x", key, now); err == nil {
		t.Fatal("tampered token unexpectedly verified")
	}
	if _, err := VerifyCustomerToken(token, key, now.Add(16*time.Minute)); err == nil {
		t.Fatal("expired token unexpectedly verified")
	}
}
