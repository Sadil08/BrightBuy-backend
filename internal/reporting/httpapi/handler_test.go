package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"brightbuy-backend/internal/reporting/app"
	"brightbuy-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
)

type handlerFakeRepository struct{}

func (handlerFakeRepository) QuarterlySales(context.Context, int) ([]app.QuarterlySalesRow, error) {
	return []app.QuarterlySalesRow{{Year: 2026, Quarter: 1, SalesValue: "100.00"}}, nil
}
func (handlerFakeRepository) TopSellingProducts(context.Context, *app.DateRange, int) ([]app.TopSellingProductRow, error) {
	return []app.TopSellingProductRow{{ProductID: 1, ProductName: "Test Product", Quantity: 2, Revenue: "20.00"}}, nil
}
func (handlerFakeRepository) CategoryWiseOrders(context.Context, *app.DateRange) ([]app.CategoryOrderCount, error) {
	return []app.CategoryOrderCount{{CategoryID: 1, Name: "Electronics", OrderCount: 2}}, nil
}
func (handlerFakeRepository) UpcomingDeliveries(context.Context) ([]app.UpcomingDeliveryRow, error) {
	return []app.UpcomingDeliveryRow{{OrderID: 1, CustomerName: "Test Customer", EstimatedDate: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}}, nil
}
func (handlerFakeRepository) CustomerOrderPayments(context.Context, *app.DateRange, *int64) ([]app.CustomerOrderPaymentRow, error) {
	return []app.CustomerOrderPaymentRow{{CustomerID: 1, OrderID: 1, OrderDate: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}}, nil
}

func testRouter() http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r, NewHandler(app.NewService(handlerFakeRepository{})), auth.RequirePermission("reports:view"))
	return r
}

func withReportsPermission(req *http.Request) *http.Request {
	return req.WithContext(auth.ContextWithClaims(req.Context(), &auth.Claims{UserID: 1, Role: "MANAGER", Permissions: []string{"reports:view"}}))
}

func TestReportsForbiddenWithoutPermission(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/quarterly-sales?year=2026", nil)
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestQuarterlySalesCSVHasJSONFieldHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/quarterly-sales?year=2026&format=csv", nil)
	req = withReportsPermission(req)
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected CSV header and at least one row, got %q", rec.Body.String())
	}
	if lines[0] != "year,quarter,salesValue" {
		t.Fatalf("CSV header = %q", lines[0])
	}
}

func TestCSVZeroRowsStillContainsHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/top-selling-products?format=csv", nil)
	req = withReportsPermission(req)
	rec := httptest.NewRecorder()
	r := chi.NewRouter()
	RegisterRoutes(r, NewHandler(app.NewService(emptyReportRepository{})), auth.RequirePermission("reports:view"))
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if strings.TrimSpace(rec.Body.String()) != "productId,productName,quantitySold,revenue" {
		t.Fatalf("unexpected empty CSV: %q", rec.Body.String())
	}
}

type emptyReportRepository struct{}

func (emptyReportRepository) QuarterlySales(context.Context, int) ([]app.QuarterlySalesRow, error) {
	return nil, nil
}
func (emptyReportRepository) TopSellingProducts(context.Context, *app.DateRange, int) ([]app.TopSellingProductRow, error) {
	return nil, nil
}
func (emptyReportRepository) CategoryWiseOrders(context.Context, *app.DateRange) ([]app.CategoryOrderCount, error) {
	return nil, nil
}
func (emptyReportRepository) UpcomingDeliveries(context.Context) ([]app.UpcomingDeliveryRow, error) {
	return nil, nil
}
func (emptyReportRepository) CustomerOrderPayments(context.Context, *app.DateRange, *int64) ([]app.CustomerOrderPaymentRow, error) {
	return nil, nil
}

func TestCategoryReportIncludesRequiredNote(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/category-wise-orders", nil)
	req = withReportsPermission(req)
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var report app.CategoryWiseReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if report.Note == "" {
		t.Fatal("category report note is empty")
	}
}
