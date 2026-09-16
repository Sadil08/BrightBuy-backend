// Package logging sets up the application's one structured logger.
//
// Every log line carries fields (key/value pairs), not just a free-text message — that's what
// "structured" means, and it's what lets a hosting platform's log viewer filter/search logs
// instead of grepping strings (specs/global/06_ENGINEERING_STANDARDS.md §5: "SLF4J structured
// logging" — same idea, Go's standard-library equivalent is log/slog).
package logging

import (
	"log/slog"
	"os"
)

// New returns a logger appropriate for env:
//   - "production"/"staging": JSON output, so the hosting platform's log collector can parse it.
//   - anything else (local dev): human-readable text output.
//
// This is the ONLY place in the codebase that decides the log format — every other package just
// calls slog.Info/slog.Error and doesn't know or care where the bytes end up.
func New(env string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	var handler slog.Handler
	if env == "production" || env == "staging" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}
