package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	cartapp "brightbuy-backend/internal/cart/app"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *cartapp.Service
}

func NewHandler(service *cartapp.Service) *Handler {
	return &Handler{service: service}
}

type addItemRequest struct {
	VariantID int `json:"variantId"`
	Quantity  int `json:"quantity"`
}

type updateItemRequest struct {
	Quantity int `json:"quantity"`
}

func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	customerID, ok := authenticatedCustomerID(w, r)
	if !ok {
		return
	}
	cart, err := h.service.GetCart(r.Context(), customerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCart(w, cart)
}

func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {
	var req addItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	customerID, ok := authenticatedCustomerID(w, r)
	if !ok {
		return
	}
	if req.VariantID <= 0 || req.Quantity < 1 {
		http.Error(w, "variantId must be positive and quantity must be at least 1", http.StatusBadRequest)
		return
	}
	cart, err := h.service.AddItem(r.Context(), customerID, req.VariantID, req.Quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCart(w, cart)
}

func (h *Handler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	var req updateItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	customerID, ok := authenticatedCustomerID(w, r)
	if !ok {
		return
	}
	variantID, err := strconv.Atoi(chi.URLParam(r, "variantID"))
	if err != nil || variantID <= 0 {
		http.Error(w, "variantID must be a positive integer", http.StatusBadRequest)
		return
	}
	if req.Quantity < 1 {
		http.Error(w, "quantity must be at least 1", http.StatusBadRequest)
		return
	}
	cart, err := h.service.UpdateItemQuantity(r.Context(), customerID, variantID, req.Quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCart(w, cart)
}

func (h *Handler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	customerID, ok := authenticatedCustomerID(w, r)
	if !ok {
		return
	}
	variantID, err := strconv.Atoi(chi.URLParam(r, "variantID"))
	if err != nil || variantID <= 0 {
		http.Error(w, "variantID must be a positive integer", http.StatusBadRequest)
		return
	}
	cart, err := h.service.RemoveItem(r.Context(), customerID, variantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeCart(w, cart)
}

func RegisterRoutes(r chi.Router, h *Handler, signingKey []byte) {
	secured := r.With(func(next http.Handler) http.Handler {
		return auth.RequireCustomer(signingKey, next)
	})
	secured.Get("/cart", h.GetCart)
	secured.Post("/cart/items", h.AddItem)
	secured.Patch("/cart/items/{variantID}", h.UpdateItem)
	secured.Delete("/cart/items/{variantID}", h.DeleteItem)
}

func authenticatedCustomerID(w http.ResponseWriter, r *http.Request) (int, bool) {
	claims, ok := auth.CustomerClaims(r)
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return 0, false
	}
	return claims.CustomerID, true
}

func writeCart(w http.ResponseWriter, cart any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cart); err != nil {
		http.Error(w, "failed to encode cart", http.StatusInternalServerError)
	}
}
