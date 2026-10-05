package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"brightbuy-backend/internal/inventory/app"
	"brightbuy-backend/internal/inventory/domain"

	"github.com/go-chi/chi/v5"
)

type fakeService struct {
	items     []domain.VariantStock
	total     int
	searchErr error
	adjustErr error
}

func TestInventoryRoutesRequireStockPermission(t *testing.T) {
	handler := NewHandler(
		fakeService{},
		actorID,
	)

	router := chi.NewRouter()

	RegisterRoutes(
		router,
		handler,
		denyStockPermission,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/staff/variants",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"got status %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}
}

func TestInventoryAdjustmentRequiresStockPermission(t *testing.T) {
	handler := NewHandler(
		fakeService{},
		actorID,
	)

	router := chi.NewRouter()

	RegisterRoutes(
		router,
		handler,
		denyStockPermission,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/staff/variants/1/stock",
		strings.NewReader(
			`{"delta":5,"reason":"restock"}`,
		),
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf(
			"got status %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}
}

func withVariantID(
	request *http.Request,
	variantID string,
) *http.Request {
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("variantId", variantID)

	request = request.WithContext(
		context.WithValue(
			request.Context(),
			chi.RouteCtxKey,
			routeContext,
		),
	)

	return request
}

func (f fakeService) Search(
	context.Context,
	string,
	int,
	int,
) ([]domain.VariantStock, int, error) {
	return f.items, f.total, f.searchErr
}

func (f fakeService) Adjust(
	context.Context,
	int,
	int,
	int,
	string,
) error {
	return f.adjustErr
}

func actorID(context.Context) (int, bool) {
	return 7, true
}

func TestSearchReturnsExactStockQuantity(t *testing.T) {
	handler := NewHandler(
		fakeService{
			items: []domain.VariantStock{
				{
					VariantID:     1,
					SKU:           "BB-1001",
					ProductName:   "Test Product",
					StockQuantity: 7,
				},
			},
			total: 1,
		},
		actorID,
	)

	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/staff/variants?q=BB-1001",
		nil,
	)

	handler.Search(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"got status %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	if !strings.Contains(
		recorder.Body.String(),
		`"stockQuantity":7`,
	) {
		t.Fatalf(
			"response did not contain exact stock quantity: %s",
			recorder.Body.String(),
		)
	}
}

func TestAdjustReturnsConflictForBelowZero(t *testing.T) {
	handler := NewHandler(
		fakeService{
			adjustErr: app.ErrAdjustmentBelowZero,
		},
		actorID,
	)

	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/staff/variants/1/stock",
		strings.NewReader(
			`{"delta":-5,"reason":"correction"}`,
		),
	)

	request = withVariantID(request, "1")

	handler.Adjust(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf(
			"got status %d, want %d",
			recorder.Code,
			http.StatusConflict,
		)
	}
}

func TestAdjustReturnsInternalError(t *testing.T) {
	handler := NewHandler(
		fakeService{
			adjustErr: errors.New("database unavailable"),
		},
		actorID,
	)

	recorder := httptest.NewRecorder()

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/staff/variants/1/stock",
		strings.NewReader(
			`{"delta":5,"reason":"restock"}`,
		),
	)

	request = withVariantID(request, "1")

	handler.Adjust(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(
			"got status %d, want %d",
			recorder.Code,
			http.StatusInternalServerError,
		)
	}
}
