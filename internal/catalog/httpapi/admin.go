package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/httpx"
	"brightbuy-backend/internal/shared/money"
)

type AdminHandler struct {
	service *app.CatalogAdminService
}

func NewAdminHandler(service *app.CatalogAdminService) *AdminHandler {
	return &AdminHandler{service: service}
}

// RegisterAdminRoutes mounts the staff catalogue routes. guard authenticates the caller and checks
// `catalog:write` (composed in main.go, like inventory's). Full paths are registered on r instead of
// r.Route("/api/v1/staff"), because chi panics if two modules each mount the same prefix.
func RegisterAdminRoutes(r chi.Router, handler *AdminHandler, guard func(http.Handler) http.Handler) {
	r.Group(func(r chi.Router) {
		r.Use(guard)
		r.Post("/api/v1/staff/products", handler.createProduct)
		r.Patch("/api/v1/staff/products/{productID}", handler.updateProduct)
		r.Delete("/api/v1/staff/products/{productID}", handler.deleteProduct)
		r.Post("/api/v1/staff/products/{productID}/variants", handler.createVariant)
		r.Patch("/api/v1/staff/products/{productID}/variants/{variantID}", handler.updateVariant)
		r.Delete("/api/v1/staff/products/{productID}/variants/{variantID}", handler.deleteVariant)
		r.Post("/api/v1/staff/categories", handler.createCategory)
		r.Patch("/api/v1/staff/categories/{categoryID}", handler.updateCategory)
		r.Delete("/api/v1/staff/categories/{categoryID}", handler.deleteCategory)
	})
}

func (h *AdminHandler) createProduct(w http.ResponseWriter, r *http.Request) {
	var request productCreateRequest
	if !decode(w, r, &request) {
		return
	}
	product, err := h.service.CreateProduct(r.Context(), request.toInput())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, adminProductResponse{ProductID: product.ID, Name: product.Name, Description: product.Description, Active: product.Active})
}

func (h *AdminHandler) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	var request productPatchRequest
	if !decode(w, r, &request) {
		return
	}
	if err := h.service.UpdateProduct(r.Context(), id, app.ProductPatch{Name: request.Name, Description: request.Description}); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) deleteProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	if err := h.service.DeactivateProduct(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) createVariant(w http.ResponseWriter, r *http.Request) {
	productID, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	var request variantCreateRequest
	if !decode(w, r, &request) {
		return
	}
	variant, err := h.service.CreateVariant(r.Context(), productID, request.toInput())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, adminVariantResponse{VariantID: variant.ID, ProductID: variant.ProductID, SKU: variant.SKU, Price: money.FromCents(variant.PriceCents), Active: variant.Active})
}

func (h *AdminHandler) updateVariant(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "variantID")
	if !ok {
		return
	}
	var request variantPatchRequest
	if !decode(w, r, &request) {
		return
	}
	patch := app.VariantPatch{}
	if request.Price != nil {
		cents := request.Price.Cents()
		patch.PriceCents = &cents
	}
	if err := h.service.UpdateVariant(r.Context(), id, patch); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) deleteVariant(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "variantID")
	if !ok {
		return
	}
	if err := h.service.DeactivateVariant(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) createCategory(w http.ResponseWriter, r *http.Request) {
	var request categoryRequest
	if !decode(w, r, &request) {
		return
	}
	input := app.CategoryInput{}
	if request.Name != nil {
		input.Name = *request.Name
	}
	if request.Description != nil {
		input.Description = *request.Description
	}
	category, err := h.service.CreateCategory(r.Context(), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, adminCategoryResponse{CategoryID: category.ID, Name: category.Name, Description: category.Description, Active: category.Active})
}

func (h *AdminHandler) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "categoryID")
	if !ok {
		return
	}
	var request categoryRequest
	if !decode(w, r, &request) {
		return
	}
	if err := h.service.UpdateCategory(r.Context(), id, app.CategoryPatch{Name: request.Name, Description: request.Description}); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminHandler) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "categoryID")
	if !ok {
		return
	}
	if err := h.service.DeactivateCategory(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_ID", "resource ID is invalid")
		return 0, false
	}
	return id, true
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		httpx.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
	case errors.Is(err, domain.ErrSKUConflict):
		httpx.WriteError(w, http.StatusConflict, "SKU_CONFLICT", "SKU already exists")
	case errors.Is(err, domain.ErrCategoryInactive):
		httpx.WriteError(w, http.StatusBadRequest, "CATEGORY_INVALID", "category is missing or inactive")
	case errors.Is(err, domain.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
