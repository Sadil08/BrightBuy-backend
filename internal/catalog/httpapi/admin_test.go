package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/auth"
)

type catalogRepositoryStub struct {
	createVariantErr error
	productErr       error
	productActive    bool
}

func (s *catalogRepositoryStub) CreateProduct(context.Context, app.AdminProduct, []int64, []app.VariantInput) (app.AdminProduct, error) {
	return app.AdminProduct{ID: 1, Name: "Created", Active: true}, s.productErr
}
func (s *catalogRepositoryStub) UpdateProduct(context.Context, int64, app.ProductPatch) error {
	return s.productErr
}
func (s *catalogRepositoryStub) SetProductActive(_ context.Context, _ int64, active bool) error {
	s.productActive = active
	return s.productErr
}
func (s *catalogRepositoryStub) CreateVariant(context.Context, int64, app.VariantInput) (app.AdminVariant, error) {
	return app.AdminVariant{}, s.createVariantErr
}
func (s *catalogRepositoryStub) UpdateVariant(context.Context, int64, app.VariantPatch) error {
	return nil
}
func (s *catalogRepositoryStub) SetVariantActive(context.Context, int64, bool) error { return nil }
func (s *catalogRepositoryStub) CreateCategory(context.Context, app.CategoryInput) (app.AdminCategory, error) {
	return app.AdminCategory{ID: 1, Name: "Created", Active: true}, nil
}
func (s *catalogRepositoryStub) UpdateCategory(context.Context, int64, app.CategoryPatch) error {
	return nil
}
func (s *catalogRepositoryStub) SetCategoryActive(context.Context, int64, bool) error { return nil }
func (s *catalogRepositoryStub) ActiveCategoriesExist(context.Context, []int64) (bool, error) {
	return true, nil
}

func adminTestRouter(repo app.Repository) chi.Router {
	r := chi.NewRouter()
	RegisterAdminRoutes(r, NewAdminHandler(app.NewCatalogAdminService(repo, nil)), auth.RequirePermission("catalog:write"))
	return r
}

func authorizedRequest(method, path, body string, permissions ...string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	claims := &auth.Claims{Permissions: permissions}
	return req.WithContext(auth.ContextWithClaims(req.Context(), claims))
}

func TestAdminRoutesRequireCatalogWritePermission(t *testing.T) {
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/staff/products", `{}`},
		{http.MethodPatch, "/api/v1/staff/products/1", `{}`},
		{http.MethodDelete, "/api/v1/staff/products/1", ""},
		{http.MethodPost, "/api/v1/staff/products/1/variants", `{}`},
		{http.MethodPatch, "/api/v1/staff/products/1/variants/1", `{}`},
		{http.MethodDelete, "/api/v1/staff/products/1/variants/1", ""},
		{http.MethodPost, "/api/v1/staff/categories", `{}`},
		{http.MethodPatch, "/api/v1/staff/categories/1", `{}`},
		{http.MethodDelete, "/api/v1/staff/categories/1", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			req := httptest.NewRequest(testCase.method, testCase.path, bytes.NewBufferString(testCase.body))
			response := httptest.NewRecorder()
			adminTestRouter(&catalogRepositoryStub{}).ServeHTTP(response, req)
			if response.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d", response.Code)
			}
		})
	}
}

func TestCreateProductReturnsCreated(t *testing.T) {
	req := authorizedRequest(http.MethodPost, "/api/v1/staff/products", `{"name":"Phone","category_ids":[1],"variants":[{"sku":"PHONE-1","price_cents":100}]}`, "catalog:write")
	response := httptest.NewRecorder()
	adminTestRouter(&catalogRepositoryStub{}).ServeHTTP(response, req)
	if response.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", response.Code, response.Body.String())
	}
}

func TestAdminHandlerMapsValidationConflictAndNotFound(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		repository *catalogRepositoryStub
		wantStatus int
	}{
		{"invalid JSON", http.MethodPost, "/api/v1/staff/categories", "{", &catalogRepositoryStub{}, http.StatusBadRequest},
		{"SKU conflict", http.MethodPost, "/api/v1/staff/products/1/variants", `{"sku":"DUP","price_cents":100}`, &catalogRepositoryStub{createVariantErr: domain.ErrSKUConflict}, http.StatusConflict},
		{"not found", http.MethodDelete, "/api/v1/staff/products/99", "", &catalogRepositoryStub{productErr: domain.ErrNotFound}, http.StatusNotFound},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			req := authorizedRequest(testCase.method, testCase.path, testCase.body, "catalog:write")
			response := httptest.NewRecorder()
			adminTestRouter(testCase.repository).ServeHTTP(response, req)
			if response.Code != testCase.wantStatus {
				t.Fatalf("expected %d, got %d: %s", testCase.wantStatus, response.Code, response.Body.String())
			}
		})
	}
}

func TestDeleteProductSoftDeactivates(t *testing.T) {
	repo := &catalogRepositoryStub{}
	req := authorizedRequest(http.MethodDelete, "/api/v1/staff/products/1", "", "catalog:write")
	response := httptest.NewRecorder()
	adminTestRouter(repo).ServeHTTP(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", response.Code)
	}
	if repo.productActive {
		t.Fatal("expected delete route to pass false to repository")
	}
}

var _ app.Repository = (*catalogRepositoryStub)(nil)
