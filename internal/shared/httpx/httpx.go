// Package httpx holds the response helpers every handler in every module shares: writing JSON,
// and writing the one error shape specs/global/01_TECH_STACK.md §3 defines —
// {"code": "...", "message": "...", "fields": [...]}. A handler in catalog and a handler in
// ordering both call httpx.WriteError, so a client sees the exact same failure shape everywhere,
// regardless of which module produced it.
package httpx

import (
	"encoding/json"
	"net/http"
)

// FieldError describes one invalid field in a request body — used when a validation failure
// needs to point at more than one problem at once.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ErrorResponse is the ONE error shape this API ever returns. Never a raw Go error string, never
// a stack trace (specs/global/02_SECURITY_BASELINE.md §4).
type ErrorResponse struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Fields  []FieldError `json:"fields,omitempty"`
}

// WriteJSON writes v as a JSON response body with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the standard error shape. code is a short machine-readable token
// (e.g. "STOCK_EXCEEDED"); message is human-readable.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorResponse{Code: code, Message: message})
}
