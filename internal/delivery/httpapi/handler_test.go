package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brightbuy-backend/internal/delivery/app"
	"brightbuy-backend/internal/delivery/domain"
)

type fakeDeliveryService struct {
	estimate domain.Estimate
	err      error
	cities   []domain.City
}

func (f fakeDeliveryService) ListCities(context.Context) ([]domain.City, error) {
	return f.cities, f.err
}

func (f fakeDeliveryService) Preview(
	_ context.Context,
	_ domain.DeliveryMode,
	_ *int,
	_ []domain.LineInput,
) (domain.Estimate, error) {
	return f.estimate, f.err
}

func newRequest(method, body string) *http.Request {
	return httptest.NewRequest(method, "/api/v1/delivery/estimate", strings.NewReader(body))
}

func TestPreviewValidStandardDelivery(t *testing.T) {
	testEstimate := domain.Estimate{
		Mode:          domain.StandardDelivery,
		EstimatedDate: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		EstimatedDays: 5,
	}
	handler := NewHandler(fakeDeliveryService{estimate: testEstimate})
	recorder := httptest.NewRecorder()

	handler.Preview(recorder, newRequest(http.MethodPost, `{
		"mode": "StandardDelivery",
		"cityId": 1,
		"items": [{"variantId": 1, "quantity": 1}]
	}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
	}

	assertEstimateResponse(t, recorder)
}

func TestPreviewValidStorePickup(t *testing.T) {
	testEstimate := domain.Estimate{
		Mode:          domain.StorePickup,
		EstimatedDate: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
		EstimatedDays: 0,
	}
	handler := NewHandler(fakeDeliveryService{estimate: testEstimate})
	recorder := httptest.NewRecorder()

	handler.Preview(recorder, newRequest(http.MethodPost, `{
		"mode": "StorePickup",
		"cityId": 1,
		"items": [{"variantId": 1, "quantity": 1}]
	}`))

	if recorder.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusOK)
	}

	assertEstimateResponse(t, recorder)
}

func TestPreviewResponseFields(t *testing.T) {
	handler := NewHandler(fakeDeliveryService{estimate: domain.Estimate{
		Mode:          domain.StandardDelivery,
		EstimatedDate: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC),
		EstimatedDays: 5,
	}})
	recorder := httptest.NewRecorder()

	handler.Preview(recorder, newRequest(http.MethodPost, `{
		"mode": "StandardDelivery",
		"cityId": 1,
		"items": [{"variantId": 1, "quantity": 1}]
	}`))

	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	for _, field := range []string{"mode", "estimatedDate", "estimatedDays"} {
		if _, ok := response[field]; !ok {
			t.Errorf("response is missing field %q", field)
		}
	}
}

func TestPreviewInvalidRequestsReturnBadRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "malformed JSON",
			body: `{"mode": "StandardDelivery"`,
		},
		{
			name: "unknown JSON field",
			body: `{"mode": "StandardDelivery", "unexpected": true}`,
		},
		{
			name: "missing city",
			body: `{"mode": "StandardDelivery", "items": [{"variantId": 1, "quantity": 1}]}`,
		},
		{
			name: "invalid quantity",
			body: `{"mode": "StandardDelivery", "cityId": 1, "items": [{"variantId": 1, "quantity": 0}]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := fakeDeliveryService{err: app.ErrInvalidRequest}
			handler := NewHandler(service)
			recorder := httptest.NewRecorder()

			handler.Preview(recorder, newRequest(http.MethodPost, test.body))

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("got status %d, want %d", recorder.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestPreviewServiceFailureReturnsInternalServerError(t *testing.T) {
	handler := NewHandler(fakeDeliveryService{err: errors.New("database unavailable")})
	recorder := httptest.NewRecorder()

	handler.Preview(recorder, newRequest(http.MethodPost, `{
		"mode": "StandardDelivery",
		"cityId": 1,
		"items": [{"variantId": 1, "quantity": 1}]
	}`))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func assertEstimateResponse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	var response struct {
		Mode          domain.DeliveryMode `json:"mode"`
		EstimatedDate string              `json:"estimatedDate"`
		EstimatedDays int                 `json:"estimatedDays"`
	}

	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Mode == "" {
		t.Error("response mode is empty")
	}
	if response.EstimatedDate == "" {
		t.Error("response estimatedDate is empty")
	}
	if response.EstimatedDays < 0 {
		t.Errorf("response estimatedDays = %d, want non-negative value", response.EstimatedDays)
	}
}

/*This code is a set of unit tests for the HTTP handler of the delivery-estimation API. It creates a
fakeDeliveryService so the tests don't need the real database or delivery logic. The tests send HTTP
POST requests containing delivery mode, city, and items, then check whether the handler returns the
correct response. The valid tests check Standard Delivery and Store Pickup, while other tests check
that malformed JSON, unknown fields, missing city, and invalid quantities return 400 Bad Request. Another
test verifies that a service/database failure returns 500 Internal Server Error, and assertEstimateResponse()
checks that the successful response contains valid mode, estimatedDate, and estimatedDays fields.*/

func TestListCitiesReturnsIDAndNameOnly(t *testing.T) {
	handler := NewHandler(fakeDeliveryService{cities: []domain.City{{ID: 2, Name: "Austin"}, {ID: 1, Name: "Dallas"}}})
	rec := httptest.NewRecorder()
	handler.ListCities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cities", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	want := `[{"cityId":2,"name":"Austin"},{"cityId":1,"name":"Dallas"}]`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body %s, want %s", got, want)
	}
}

func TestListCitiesEmptyIsAnEmptyArrayNotNull(t *testing.T) {
	handler := NewHandler(fakeDeliveryService{})
	rec := httptest.NewRecorder()
	handler.ListCities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cities", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body %s, want []", got)
	}
}

func TestListCitiesServiceErrorIs500(t *testing.T) {
	handler := NewHandler(fakeDeliveryService{err: context.DeadlineExceeded})
	rec := httptest.NewRecorder()
	handler.ListCities(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cities", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
}
