package config

import "testing"

const goodKey = "0123456789abcdef0123456789abcdef" // 32 bytes

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DB_DSN", "user:pass@tcp(localhost:3306)/db")
	t.Setenv("JWT_SIGNING_KEY", goodKey)
	t.Setenv("ADDR", "")
	t.Setenv("PORT", "")
}

func TestLoadListenAddress(t *testing.T) {
	cases := []struct{ name, addr, port, want string }{
		{"default", "", "", ":8080"},
		{"PORT is used when the host provides it", "", "10000", ":10000"},
		{"ADDR wins over PORT", "127.0.0.1:9000", "10000", "127.0.0.1:9000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("ADDR", c.addr)
			t.Setenv("PORT", c.port)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Addr != c.want {
				t.Errorf("Addr = %q, want %q", cfg.Addr, c.want)
			}
		})
	}
}

func TestLoadRequiresDSNAndASufficientlyLongSigningKey(t *testing.T) {
	setRequired(t)
	t.Setenv("DB_DSN", "")
	if _, err := Load(); err == nil {
		t.Error("missing DB_DSN should fail fast")
	}

	setRequired(t)
	t.Setenv("JWT_SIGNING_KEY", "too-short")
	if _, err := Load(); err == nil {
		t.Error("a signing key under 32 bytes should be refused")
	}

	setRequired(t)
	if _, err := Load(); err != nil {
		t.Errorf("valid configuration refused: %v", err)
	}
}

func TestLoadPassesTheDatabaseCACertThrough(t *testing.T) {
	setRequired(t)
	cfg, err := Load()
	if err != nil || cfg.DatabaseCACert != "" {
		t.Fatalf("unset DB_CA_CERT should be empty and valid, got %q, %v", cfg.DatabaseCACert, err)
	}

	t.Setenv("DB_CA_CERT", "-----BEGIN CERTIFICATE-----\nabc\n-----END CERTIFICATE-----")
	cfg, err = Load()
	if err != nil || cfg.DatabaseCACert == "" {
		t.Fatalf("DB_CA_CERT was not read: %q, %v", cfg.DatabaseCACert, err)
	}
}
