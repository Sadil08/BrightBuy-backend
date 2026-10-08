// Package config loads every environment-derived setting the application needs at startup.
//
// It is read exactly once, in main(), and the resulting Config is passed down explicitly to
// whatever needs it. No other package anywhere in this codebase calls os.Getenv directly — that
// is the whole point of this package existing (specs/global/01_TECH_STACK.md §3: "configuration
// from environment, never from committed source").
package config

import (
	"fmt"
	"os"
)

// Config is a plain struct, not a singleton — it gets constructed once in main() and threaded
// through constructors from there (specs/global/06_ENGINEERING_STANDARDS.md §3: no global state).
type Config struct {
	// Addr is the address the HTTP server listens on, e.g. ":8080".
	Addr string
	// Env is "local", "staging", or "production" — controls logging format (see logging package)
	// and nothing else; business logic must never branch on this.
	Env string
	// DatabaseDSN is the MySQL "Data Source Name" the driver connects with.
	DatabaseDSN string
	// JWTSigningKey signs and verifies access/refresh tokens (specs/global/02_SECURITY_BASELINE.md §2).
	JWTSigningKey string
}

// Load reads the environment and returns a populated Config, or an error if something required
// is missing. Failing fast here — at startup, with a clear message — is much better than a nil
// pointer panic three requests into serving traffic.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:          getEnv("ADDR", ":8080"),
		Env:           getEnv("APP_ENV", "local"),
		DatabaseDSN:   os.Getenv("DB_DSN"),
		JWTSigningKey: os.Getenv("JWT_SIGNING_KEY"),
	}

	if cfg.DatabaseDSN == "" {
		return nil, fmt.Errorf("DB_DSN environment variable is required")
	}
	// JWT signing key must be at least 32 bytes long for security reasons
	if len(cfg.JWTSigningKey) < 32 {
		return nil, fmt.Errorf("JWT_SIGNING_KEY must contain at least 32 bytes")
	}

	return cfg, nil
}

// getEnv returns the environment variable named key, or fallback if it is unset or empty.
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
