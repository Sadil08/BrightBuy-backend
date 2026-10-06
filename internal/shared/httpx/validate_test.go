package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testRequestDTO struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

func TestDecodeAndValidateAcceptsValidBody(t *testing.T) {
	body := strings.NewReader(`{"email":"a@example.com","password":"password123"}`)
	req := httptest.NewRequest(http.MethodPost, "/", body)

	got, err := DecodeAndValidate[testRequestDTO](req)
	if err != nil {
		t.Fatalf("DecodeAndValidate: %v", err)
	}
	if got.Email != "a@example.com" {
		t.Errorf("Email = %q, want a@example.com", got.Email)
	}
}

func TestDecodeAndValidateRejectsShortPassword(t *testing.T) {
	body := strings.NewReader(`{"email":"a@example.com","password":"short"}`)
	req := httptest.NewRequest(http.MethodPost, "/", body)

	_, err := DecodeAndValidate[testRequestDTO](req)
	if err == nil {
		t.Fatal("DecodeAndValidate accepted a 5-character password, want a validation error (min=8)")
	}

	rec := httptest.NewRecorder()
	WriteValidationError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "Password") {
		t.Errorf("body = %s, want it to name the Password field", rec.Body.String())
	}
}

func TestDecodeAndValidateRejectsMalformedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`not json at all`))

	_, err := DecodeAndValidate[testRequestDTO](req)
	if err == nil {
		t.Fatal("DecodeAndValidate accepted malformed JSON, want an error")
	}

	rec := httptest.NewRecorder()
	WriteValidationError(rec, err)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
