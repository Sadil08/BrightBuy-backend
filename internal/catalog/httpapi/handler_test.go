package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/money"
)

// fakeProductRepository/fakeCategoryRepository are local to this test file, same idea as
// app/catalog_service_test.go's fakes — this package can't reuse those directly (they're unexported
// to the app package's own tests), but the pattern is identical: a hand-written stand-in satisfying
// app.ProductRepository/app.CategoryRepository, so these handler tests never need a real database.
type fakeProductRepository struct {
	products []domain.Product
	total    int
	byID     map[int]domain.Product
}

func (f *fakeProductRepository) List(ctx context.Context, filter app.ListFilter) ([]domain.Product, int, error) {
	return f.products, f.total, nil
}

func (f *fakeProductRepository) GetByID(ctx context.Context, id int) (*domain.Product, error) {
	p, ok := f.byID[id]
	if !ok {
		return nil, app.ErrNotFound
	}
	return &p, nil
}

func (f *fakeProductRepository) GetVariantForCart(
	context.Context,
	int,
) (*app.CartVariant, error) {
	return nil, app.ErrNotFound
}

type fakeCategoryRepository struct {
	categories []domain.Category
}

func (f *fakeCategoryRepository) List(ctx context.Context) ([]domain.Category, error) {
	return f.categories, nil
}

// newTestRouter builds a real chi router with RegisterRoutes wired in — using the actual router
// (rather than calling h.GetProduct(w, r) directly) is what makes chi.URLParam(r, "productId")
// inside GetProduct actually work: chi only knows how to fill that in because a real route with a
// {productId} segment matched the request, which only happens by going through the router.
func newTestRouter(h *CatalogHandler) http.Handler {
	r := chi.NewRouter()
	RegisterRoutes(r, h)
	return r
}

func mustParseMoney(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s)
	if err != nil {
		t.Fatalf("money.Parse(%q): %v", s, err)
	}
	return m
}

func TestListCategories_ReturnsActiveCategoriesAsJSON(t *testing.T) {
	repo := &fakeCategoryRepository{categories: []domain.Category{
		{ID: 1, Name: "Laptops", Description: "Portable computers"},
	}}
	service := app.NewCatalogService(&fakeProductRepository{}, repo)
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []CategoryDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 || got[0].Name != "Laptops" {
		t.Errorf("got %+v, want one CategoryDTO{ID: 1, Name: \"Laptops\", ...}", got)
	}
}

func TestListProducts_ReturnsPaginationEnvelope(t *testing.T) {
	products := []domain.Product{
		{ID: 1, Name: "Widget", Variants: []domain.Variant{
			{ID: 10, SKU: "W-1", Price: mustParseMoney(t, "9.99"), Stock: domain.InStock},
		}},
	}
	repo := &fakeProductRepository{products: products, total: 45}
	service := app.NewCatalogService(repo, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/products?page=2&size=20", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got ProductListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// This is exactly the bug class the Normalized()/filter-by-value discussion was about: if the
	// handler read back un-normalized values, this would report page 2's REQUEST page/size correctly
	// here (since both were explicitly supplied), so this test alone wouldn't catch a regression
	// there — TestListProducts_DefaultsPageAndSizeInResponseEnvelope below covers that specific case.
	if got.Page.Page != 2 || got.Page.Size != 20 || got.Page.Total != 45 {
		t.Errorf("page = %+v, want {Page:2 Size:20 Total:45}", got.Page)
	}
	if len(got.Items) != 1 || got.Items[0].ProductID != 1 || got.Items[0].StockStatus != "IN_STOCK" {
		t.Errorf("items = %+v, want one in-stock product with ID 1", got.Items)
	}
}

// TestListProducts_DefaultsPageAndSizeInResponseEnvelope is the specific case the ListFilter.
// Normalized() fix (called from both the service AND the handler) exists for: a request with NO
// ?page=/?size= at all must still report back the REAL defaults used (1/20), not the zero values
// (0/0) that would show up if the handler's own copy of filter never got normalized.
func TestListProducts_DefaultsPageAndSizeInResponseEnvelope(t *testing.T) {
	repo := &fakeProductRepository{total: 0}
	service := app.NewCatalogService(repo, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/products", nil) // no query string at all
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var got ProductListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Page.Page != 1 || got.Page.Size != 20 {
		t.Errorf("page = %+v, want {Page:1 Size:20 ...} (the documented defaults)", got.Page)
	}
}

func TestGetProduct_Returns200WithFullDetail(t *testing.T) {
	repo := &fakeProductRepository{byID: map[int]domain.Product{
		42: {
			ID: 42, Name: "Bluetooth Speaker", Description: "Portable",
			Categories: []domain.Category{{ID: 1, Name: "Audio"}},
			Variants: []domain.Variant{
				{ID: 100, SKU: "SPK-1", Price: mustParseMoney(t, "49.99"), Stock: domain.OutOfStock, Attributes: map[string]string{}},
			},
		},
	}}
	service := app.NewCatalogService(repo, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/products/42", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got ProductDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ProductID != 42 || len(got.Variants) != 1 || got.Variants[0].StockStatus != "OUT_OF_STOCK" {
		t.Errorf("got %+v, want product 42 with one OUT_OF_STOCK variant", got)
	}
	// The specific bug fixed in mapper.go: a variant with zero attributes must serialize as `[]`,
	// never `null` — decoding into a Go slice would silently accept either, so this checks the RAW
	// bytes, which is the only way to actually distinguish them.
	if strings.Contains(rec.Body.String(), `"attributes":null`) {
		t.Error(`response contains "attributes":null — should be "attributes":[] for a variant with no attributes`)
	}
}

// TestGetProduct_SetsETagAndReturns304WhenIfNoneMatchMatches exercises the full round trip plan.md
// §1's ETag exists for: a first request gets an ETag back; replaying that EXACT ETag as If-None-Match
// (what a real browser/HTTP cache does automatically once it's seen one) gets a 304 with no body,
// instead of the full product JSON being rebuilt and resent for data the client already has.
func TestGetProduct_SetsETagAndReturns304WhenIfNoneMatchMatches(t *testing.T) {
	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeProductRepository{byID: map[int]domain.Product{
		1: {
			ID: 1, Name: "Widget", UpdatedAt: fixedTime,
			Variants: []domain.Variant{{ID: 1, SKU: "W-1", Price: mustParseMoney(t, "9.99"), Stock: domain.InStock}},
		},
	}}
	service := app.NewCatalogService(repo, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req1 := httptest.NewRequest(http.MethodGet, "/products/1", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200; body = %s", rec1.Code, rec1.Body.String())
	}
	etag := rec1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first request: no ETag header set")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/products/1", nil)
	req2.Header.Set("If-None-Match", etag) // exactly what a browser does once it's cached this ETag
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusNotModified {
		t.Errorf("second request (matching If-None-Match): status = %d, want %d", rec2.Code, http.StatusNotModified)
	}
	if rec2.Body.Len() != 0 {
		t.Errorf("304 response has a %d-byte body — should be empty, that's the entire bandwidth point of an ETag", rec2.Body.Len())
	}
}

func TestGetProduct_Returns404ForUnknownID(t *testing.T) {
	service := app.NewCatalogService(&fakeProductRepository{byID: map[int]domain.Product{}}, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/products/999", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestGetProduct_Returns400ForNonNumericID(t *testing.T) {
	service := app.NewCatalogService(&fakeProductRepository{}, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/products/not-a-number", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body = %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestSuccessResponsesSetCacheControlHeader(t *testing.T) {
	service := app.NewCatalogService(&fakeProductRepository{}, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/categories", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60" {
		t.Errorf("Cache-Control = %q, want %q (plan.md §1)", got, "public, max-age=60")
	}
}

// TestDTOsNeverDeclareAStockQuantityField is plan.md §8's "reflection-based test over the serialized
// struct": rather than checking one hand-picked JSON response for the word "quantity" (which would
// only catch today's bug, not a future one), this walks every DTO struct's ACTUAL field definitions
// via reflection and asserts none of their `json` tags could possibly leak a raw stock count. This
// is what plan.md means by "should catch a field rename as well as an obvious mistake" — if someone
// later adds a field like `StockQuantity int` to any DTO below, THIS test fails immediately, even
// before any handler ever constructs or serializes one.
func TestDTOsNeverDeclareAStockQuantityField(t *testing.T) {
	dtoTypes := []any{
		CategoryDTO{},
		VariantAttributeDTO{},
		VariantDTO{},
		ProductDTO{},
		ProductSummaryDTO{},
		PageDTO{},
		ProductListDTO{},
	}

	for _, dto := range dtoTypes {
		typ := reflect.TypeOf(dto)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			jsonTag := strings.ToLower(field.Tag.Get("json"))
			if strings.Contains(jsonTag, "quantity") {
				t.Errorf("%s.%s has json tag %q — exact stock quantity must never be a DTO field (SEC-CATALOG-1, FR-CATALOG-7)",
					typ.Name(), field.Name, field.Tag.Get("json"))
			}
		}
	}
}

func TestListProducts_PageAndSizeQueryParamsAreParsed(t *testing.T) {
	for _, size := range []int{1, 100} {
		repo := &fakeProductRepository{total: 0}
		service := app.NewCatalogService(repo, &fakeCategoryRepository{})
		router := newTestRouter(NewCatalogHandler(service))

		req := httptest.NewRequest(http.MethodGet, "/products?size="+strconv.Itoa(size), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var got ProductListDTO
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if got.Page.Size != size {
			t.Errorf("?size=%d: response page.size = %d, want %d", size, got.Page.Size, size)
		}
	}
}

// TestListProducts_InvalidPageAndSizeQueryParamsAreClamped checks that invalid pagination values
// fall back to the same defaults used when the parameters are omitted.
func TestListProducts_InvalidPageAndSizeQueryParamsAreClamped(t *testing.T) {
	service := app.NewCatalogService(&fakeProductRepository{}, &fakeCategoryRepository{})
	router := newTestRouter(NewCatalogHandler(service))

	req := httptest.NewRequest(http.MethodGet, "/products?page=-1&size=101", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var got ProductListDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Page.Page != 1 || got.Page.Size != 20 {
		t.Errorf("page = %+v, want {Page:1 Size:20 ...} for invalid query values", got.Page)
	}
}
