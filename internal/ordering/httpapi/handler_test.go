package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	issuer := auth.NewTokenIssuer("0123456789abcdef0123456789abcdef")
	service := &fakeService{}
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(service), issuer)

	req := httptest.NewRequest(http.MethodPost, "/checkout", strings.NewReader(`{"deliveryMode":"StorePickup","paymentMethod":"COD"}`))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d, want 401", rec.Code)
	}

	token, err := issuer.IssueAccessTokenForCustomer(99, 37, "CUSTOMER", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, "/checkout", strings.NewReader(`{"deliveryMode":"StorePickup","paymentMethod":"COD"}`))
	req.Header.Set("Idempotency-Key", "request-1")
	req.AddCookie(&http.Cookie{Name: auth.AccessTokenCookie, Value: token})
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
	issuer := auth.NewTokenIssuer("0123456789abcdef0123456789abcdef")
	service := &fakeService{}
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(service), issuer)
	token, err := issuer.IssueAccessTokenForCustomer(99, 37, "CUSTOMER", nil)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/orders/14", nil)
	req.AddCookie(&http.Cookie{Name: auth.AccessTokenCookie, Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || service.customerID != 37 || service.orderID != 14 {
		t.Fatalf("status=%d customer=%d order=%d body=%s", rec.Code, service.customerID, service.orderID, rec.Body.String())
	}
}

func TestOrderRoutesRejectNonCustomersAndTokensWithoutCustomerID(t *testing.T) {
	issuer := auth.NewTokenIssuer("0123456789abcdef0123456789abcdef")
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(&fakeService{}), issuer)
	cases := []struct {
		name string
		tok  func() (string, error)
		want int
	}{
		{"staff role", func() (string, error) { return issuer.IssueAccessTokenForCustomer(5, 0, "ADMIN", nil) }, http.StatusForbidden},
		{"customer token with no customer id", func() (string, error) { return issuer.IssueAccessToken(5, "CUSTOMER", nil) }, http.StatusUnauthorized},
	}
	for _, c := range cases {
		tok, err := c.tok()
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodGet, "/orders", nil)
		req.AddCookie(&http.Cookie{Name: auth.AccessTokenCookie, Value: tok})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, rec.Code, c.want)
		}
	}
}
