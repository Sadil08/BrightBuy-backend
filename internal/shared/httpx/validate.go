package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-playground/validator/v10"
)

// validatorInstance is created once, process-wide — go-playground/validator's own docs recommend
// this: it caches struct-tag reflection per type internally, so a new instance per request would
// throw that caching away for no benefit.
var validatorInstance = validator.New()

// DecodeAndValidate reads r's JSON body into a new T, then validates it against T's own `validate`
// struct tags — specs/global/02_SECURITY_BASELINE.md §4: "every request body is validated
// server-side via go-playground/validator struct tags... before it touches a service." Every
// feature's request DTOs use this the same way: `req, err := httpx.DecodeAndValidate[RegisterRequestDTO](r)`.
// The returned error is either a JSON decoding failure or a validator.ValidationErrors — pass it
// straight to WriteValidationError, which tells the two apart and responds appropriately either way.
func DecodeAndValidate[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, err
	}
	if err := validatorInstance.Struct(v); err != nil {
		return v, err
	}
	return v, nil
}

// WriteValidationError writes the standard 400 error shape for whatever DecodeAndValidate handed
// back. A validator.ValidationErrors gets the field-by-field detail (ErrorResponse.Fields) — e.g.
// {"code":"VALIDATION_FAILED","fields":[{"field":"Password","message":"min"}]} — anything else
// (malformed JSON) gets a plainer "the request body itself is bad" message, since there's no
// per-field detail to report when the body couldn't even be parsed.
func WriteValidationError(w http.ResponseWriter, err error) {
	var validationErrors validator.ValidationErrors
	if errors.As(err, &validationErrors) {
		fields := make([]FieldError, 0, len(validationErrors))
		for _, fieldErr := range validationErrors {
			fields = append(fields, FieldError{Field: fieldErr.Field(), Message: fieldErr.Tag()})
		}
		WriteJSON(w, http.StatusBadRequest, ErrorResponse{
			Code: "VALIDATION_FAILED", Message: "request validation failed", Fields: fields,
		})
		return
	}
	WriteError(w, http.StatusBadRequest, "INVALID_BODY", "malformed request body")
}
