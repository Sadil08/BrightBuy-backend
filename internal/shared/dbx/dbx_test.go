package dbx

import (
	"strings"
	"testing"
)

func TestOpenWithCARejectsAnInvalidCertificate(t *testing.T) {
	_, err := OpenWithCA("user:pass@tcp(localhost:3306)/db", "this is not a PEM certificate")
	if err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Fatalf("err = %v, want a clear error naming the PEM problem", err)
	}
}

func TestOpenWithCARejectsAMalformedDSN(t *testing.T) {
	_, err := OpenWithCA("%%% not a dsn", "-----BEGIN CERTIFICATE-----")
	if err == nil {
		t.Fatal("a malformed DSN should be refused before any connection attempt")
	}
}
