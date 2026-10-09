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

// CheckETag sets the ETag response header to etag, then checks whether the client already has this
// exact version cached (via its own If-None-Match request header, which a browser/HTTP cache sets
// automatically once it's seen this ETag before). If they match, it writes 304 Not Modified (with no
// body at all — that's the entire bandwidth-saving point of an ETag) and returns true, telling the
// caller to stop immediately rather than build and send the full response again for data the client
// already has. Returns false if the caller should proceed to write the normal 200 response as usual
// (the ETag header is already set either way, so a fresh client still gets one to cache for next time).
//
// etag should already be wrapped in quotes per RFC 9110 (e.g. `"abc123"` or the weak form
// `W/"abc123"` — see specs/global/07... no single global rule names the format, so each caller
// decides weak vs strong for its own data; catalog's product detail uses weak, since it's derived
// from a timestamp, not a byte-exact hash of the response body).
func CheckETag(w http.ResponseWriter, r *http.Request, etag string) (notModified bool) {
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	return false
}
