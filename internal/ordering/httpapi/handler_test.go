package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brightbuy-backend/internal/ordering/app"
	orderdomain "brightbuy-backend/internal/ordering/domain"
	"brightbuy-backend/internal/shared/auth"
	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	customerID int
	orderID    int
}

func (f *fakeService) PlaceOrder(_ context.Context, customerID int, _ string, _ app.PlaceOrderRequest) (*orderdomain.Order, error) {
	f.customerID = customerID
	return &orderdomain.Order{ID: 5}, nil
}
func (f *fakeService) GetOrder(_ context.Context, customerID, orderID int) (*orderdomain.Order, error) {
	f.customerID, f.orderID = customerID, orderID
	return &orderdomain.Order{ID: orderID}, nil
}
func (f *fakeService) ListOrders(_ context.Context, customerID, _, _ int) ([]orderdomain.Order, int, error) {
	f.customerID = customerID
	return []orderdomain.Order{}, 0, nil
}

func TestCheckoutRequiresCustomerSessionAndUsesClaimCustomerID(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	service := &fakeService{}
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(service), key)

	req := httptest.NewRequest(http.MethodPost, "/checkout", strings.NewReader(`{"deliveryMode":"StorePickup","paymentMethod":"COD"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want 401", rec.Code)
	}

	token, err := auth.IssueCustomerToken(key, 99, 37, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/checkout", strings.NewReader(`{"deliveryMode":"StorePickup","paymentMethod":"COD"}`))
	req.Header.Set("Idempotency-Key", "request-1")
	req.AddCookie(&http.Cookie{Name: "brightbuy_session", Value: token})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("authenticated status=%d body=%s", rec.Code, rec.Body.String())
	}
	if service.customerID != 37 {
		t.Fatalf("customer ID=%d, want signed claim 37", service.customerID)
	}
}

func TestGetOrderPassesSignedCustomerIDToOwnershipQuery(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	service := &fakeService{}
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(service), key)
	token, err := auth.IssueCustomerToken(key, 99, 37, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/orders/14", nil)
	req.AddCookie(&http.Cookie{Name: "brightbuy_session", Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || service.customerID != 37 || service.orderID != 14 {
		t.Fatalf("status=%d customer=%d order=%d body=%s", rec.Code, service.customerID, service.orderID, rec.Body.String())
	}
}
