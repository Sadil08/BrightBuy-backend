package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"brightbuy-backend/internal/ordering/app"
	"brightbuy-backend/internal/shared/auth"
	"github.com/go-chi/chi/v5"
)

type mockOrderStatusService struct {
	err error
}

func (m *mockOrderStatusService) UpdateStatus(ctx context.Context, actingUserID, orderID int, newStatus string) error {
	return m.err
}

func (m *mockOrderStatusService) Cancel(ctx context.Context, actingUserID, orderID int) error {
	return m.err
}

func TestStaffHandler_UpdateStatus_Forbidden(t *testing.T) {
	svc := &mockOrderStatusService{}
	handler := NewStaffHandler(svc)

	r := chi.NewRouter()
	r.Patch("/staff/orders/{orderId}/status", handler.UpdateStatus)

	tests := []struct {
		name       string
		status     string
		permission string
	}{
		{"advance needs order:status:update", "Processing", "other:permission"},
		{"cancel needs order:cancel", "Cancelled", "order:status:update"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"status":"` + tc.status + `"}`)
			req := httptest.NewRequest(http.MethodPatch, "/staff/orders/123/status", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			// Inject claims with wrong permission
			claims := &auth.Claims{UserID: 1, Role: "STAFF", Permissions: []string{tc.permission}}
			ctx := auth.ContextWithClaims(req.Context(), claims)
			req = req.WithContext(ctx)

			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Errorf("expected 403 Forbidden, got %d", rr.Code)
			}
		})
	}
}

func TestStaffHandler_UpdateStatus_Conflict(t *testing.T) {
	svc := &mockOrderStatusService{err: app.ErrInvalidTransition}
	handler := NewStaffHandler(svc)

	r := chi.NewRouter()
	r.Patch("/staff/orders/{orderId}/status", handler.UpdateStatus)

	body := []byte(`{"status":"Processing"}`)
	req := httptest.NewRequest(http.MethodPatch, "/staff/orders/123/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	claims := &auth.Claims{UserID: 1, Role: "STAFF", Permissions: []string{"order:status:update"}}
	ctx := auth.ContextWithClaims(req.Context(), claims)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d", rr.Code)
	}
}
