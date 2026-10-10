package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"
)

type AdminHandler struct {
	service *app.CatalogAdminService
}

func NewAdminHandler(service *app.CatalogAdminService) *AdminHandler {
	return &AdminHandler{service: service}
}

func RegisterAdminRoutes(r chi.Router, handler *AdminHandler) {
	r.Route("/api/v1/staff", func(r chi.Router) {
		r.Use(auth.RequirePermission("catalog:write"))
		r.Post("/products", handler.createProduct)
		r.Patch("/products/{productID}", handler.updateProduct)
		r.Delete("/products/{productID}", handler.deleteProduct)
		r.Post("/products/{productID}/variants", handler.createVariant)
		r.Patch("/products/{productID}/variants/{variantID}", handler.updateVariant)
		r.Delete("/products/{productID}/variants/{variantID}", handler.deleteVariant)
		r.Post("/categories", handler.createCategory)
		r.Patch("/categories/{categoryID}", handler.updateCategory)
		r.Delete("/categories/{categoryID}", handler.deleteCategory)
	})
}

func (h *AdminHandler) createProduct(w http.ResponseWriter, r *http.Request) {
	var input app.ProductInput
	if !decode(w, r, &input) {
		return
	}
	product, err := h.service.CreateProduct(r.Context(), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, product)
}

func (h *AdminHandler) updateProduct(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "productID")
	if !ok {
		return
	}
	var patch app.ProductPatch
	if !decode(w, r, &patch) {
		return
	}
	if err := h.service.UpdateProduct(r.Context(), id, patch); err != nil {
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
	var input app.VariantInput
	if !decode(w, r, &input) {
		return
	}
	variant, err := h.service.CreateVariant(r.Context(), productID, input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, variant)
}

func (h *AdminHandler) updateVariant(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "variantID")
	if !ok {
		return
	}
	var patch app.VariantPatch
	if !decode(w, r, &patch) {
		return
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
	var input app.CategoryInput
	if !decode(w, r, &input) {
		return
	}
	category, err := h.service.CreateCategory(r.Context(), input)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, category)
}

func (h *AdminHandler) updateCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "categoryID")
	if !ok {
		return
	}
	var patch app.CategoryPatch
	if !decode(w, r, &patch) {
		return
	}
	if err := h.service.UpdateCategory(r.Context(), id, patch); err != nil {
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
