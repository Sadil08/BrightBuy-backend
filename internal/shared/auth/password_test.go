package auth

import "testing"

func TestHashAndComparePasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !ComparePassword(hash, "correct-horse-battery-staple") {
		t.Error("ComparePassword with the correct plaintext returned false")
	}
	if ComparePassword(hash, "wrong-password") {
		t.Error("ComparePassword with the wrong plaintext returned true")
	}
}

func TestHashPasswordNeverReturnsThePlaintext(t *testing.T) {
	hash, err := HashPassword("my-secret-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "my-secret-password" {
		t.Fatal("HashPassword returned the plaintext unchanged — not actually hashed")
	}
}

func TestHashPasswordProducesDifferentHashesEachTime(t *testing.T) {
	// BCrypt embeds a random salt per call — hashing the same plaintext twice must never produce
	// the same stored value, or two users with the same password would be visibly identical in a
	// database dump.
	hash1, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	hash2, err := HashPassword("same-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash1 == hash2 {
		t.Error("two HashPassword calls with the same plaintext produced the same hash — salt isn't random")
	}
}
