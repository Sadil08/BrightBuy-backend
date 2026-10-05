package auth

import "testing"

func TestGenerateRefreshTokenHashMatchesHashRefreshToken(t *testing.T) {
	raw, hash, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if raw == "" || hash == "" {
		t.Fatal("GenerateRefreshToken returned an empty raw token or hash")
	}
	if raw == hash {
		t.Fatal("raw token and hash are identical — hash isn't actually hashing anything")
	}
	// The whole point of SEC-AUTH-2: re-hashing the raw value later (what /auth/refresh does with
	// the cookie it receives) must reproduce the exact same hash that was stored at issue time.
	if got := HashRefreshToken(raw); got != hash {
		t.Errorf("HashRefreshToken(raw) = %q, want %q (GenerateRefreshToken's own hash)", got, hash)
	}
}

func TestGenerateRefreshTokenProducesDistinctTokens(t *testing.T) {
	raw1, _, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	raw2, _, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken: %v", err)
	}
	if raw1 == raw2 {
		t.Error("two GenerateRefreshToken calls produced the same token")
	}
}

func TestGenerateRandomPasswordMeetsMinimumLength(t *testing.T) {
	password, err := GenerateRandomPassword()
	if err != nil {
		t.Fatalf("GenerateRandomPassword: %v", err)
	}
	// FR-AUTH-7: minimum 8 characters — the generated password needs to comfortably clear whatever
	// validation registration/account-creation applies to a human-chosen one.
	if len(password) < 8 {
		t.Errorf("GenerateRandomPassword length = %d, want >= 8", len(password))
	}
}
