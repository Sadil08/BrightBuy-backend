// This file is the HTTP layer: it reads the request, calls the cart service, and writes JSON in the
// shape specs/openapi/openapi.yaml defines. The customer is never taken from the request — it is
// always the logged-in user from the JWT claims (SEC-CART-1), and no price is ever accepted from the
// client (SEC-CART-2): the DTOs below simply have no price field and the server ignores unknown ones.

package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	cartapp "brightbuy-backend/internal/cart/app"
	"brightbuy-backend/internal/cart/domain"
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

// CartItemInputDTO is OpenAPI's CartItemInput: {variantId, quantity >= 1}.
type CartItemInputDTO struct {
	VariantID int `json:"variantId" validate:"required,gt=0"`
	Quantity  int `json:"quantity" validate:"required,gte=1,lte=1000"` // 1000 = domain.MaxLineQuantity
}

// UpdateItemRequestDTO: quantity is a pointer so an omitted field is rejected rather than read as 0
// (and 0 means "remove the line", so silently defaulting to it would delete data).
type UpdateItemRequestDTO struct {
	Quantity *int `json:"quantity" validate:"required,gte=0,lte=1000"`
}

type MergeRequestDTO struct {
	Items []CartItemInputDTO `json:"items" validate:"required,max=100,dive"`
}

// customerID returns the logged-in customer's id from the verified JWT claims (set by auth.Authenticate).
func (h *Handler) customerID(w http.ResponseWriter, r *http.Request) (int, bool) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return 0, false
	}
	if claims.CustomerID > 0 {
		return claims.CustomerID, true // normal path: no database round trip
	}
	// Fallback for access tokens issued before the customer id was added to the JWT (<= 15 min
	// old): look it up once. Also what returns 403 for accounts with no customer profile.
	id, err := h.service.CustomerIDForUser(r.Context(), claims.UserID)
	if errors.Is(err, domain.ErrNotCustomer) {
		httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "only customers have a cart")
		return 0, false
	}
	if err != nil {
		h.internalError(w, r, err)
		return 0, false
	}
	return id, true
}

func (h *Handler) cartItemID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(chi.URLParam(r, "cartItemId"))
	if err != nil || id <= 0 {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_ID", "cartItemId must be a positive integer")
		return 0, false
	}
	return id, true
}

// GetCart handles GET /cart.
func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {
	customerID, ok := h.customerID(w, r)
	if !ok {
		return
	}
	cart, err := h.service.GetCart(r.Context(), customerID)
	h.respond(w, r, cart, err)
}

// AddItem handles POST /cart/items.
func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {
	customerID, ok := h.customerID(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeAndValidate[CartItemInputDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}
	cart, err := h.service.AddItem(r.Context(), customerID, req.VariantID, req.Quantity)
	h.respond(w, r, cart, err)
}

// UpdateItem handles PATCH /cart/items/{cartItemId}.
func (h *Handler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	customerID, ok := h.customerID(w, r)
	if !ok {
		return
	}
	cartItemID, ok := h.cartItemID(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeAndValidate[UpdateItemRequestDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}
	cart, err := h.service.UpdateItemQuantity(r.Context(), customerID, cartItemID, *req.Quantity)
	h.respond(w, r, cart, err)
}

// DeleteItem handles DELETE /cart/items/{cartItemId}.
func (h *Handler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	customerID, ok := h.customerID(w, r)
	if !ok {
		return
	}
	cartItemID, ok := h.cartItemID(w, r)
	if !ok {
		return
	}
	cart, err := h.service.RemoveItem(r.Context(), customerID, cartItemID)
	h.respond(w, r, cart, err)
}

// Merge handles POST /cart/merge: the guest's browser cart folded into the server cart on login.
func (h *Handler) Merge(w http.ResponseWriter, r *http.Request) {
	customerID, ok := h.customerID(w, r)
	if !ok {
		return
	}
	req, err := httpx.DecodeAndValidate[MergeRequestDTO](r)
	if err != nil {
		httpx.WriteValidationError(w, err)
		return
	}
	items := make([]cartapp.LineInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = cartapp.LineInput{VariantID: it.VariantID, Quantity: it.Quantity}
	}
	cart, err := h.service.Merge(r.Context(), customerID, items)
	h.respond(w, r, cart, err)
}

// respond writes the cart on success, or the standard error shape for a known failure.
func (h *Handler) respond(w http.ResponseWriter, r *http.Request, cart *domain.Cart, err error) {
	var stock *domain.StockExceededError
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, cart)
	case errors.As(err, &stock):
		httpx.WriteError(w, http.StatusBadRequest, "STOCK_EXCEEDED", stock.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
	case errors.Is(err, domain.ErrVariantNotFound):
		httpx.WriteError(w, http.StatusNotFound, "VARIANT_NOT_FOUND", "variant not found")
	case errors.Is(err, domain.ErrLineNotFound):
		httpx.WriteError(w, http.StatusNotFound, "CART_ITEM_NOT_FOUND", "cart item not found")
	default:
		h.internalError(w, r, err)
	}
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, err error) {
	slog.ErrorContext(r.Context(), "cart request failed", "error", err)
	httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
}

// RegisterRoutes wires the five cart endpoints (plan.md §3). Every one requires a valid session and
// the CUSTOMER role. Group scopes that middleware to these routes only: calling Use on the shared
// router would panic (chi wants middleware before any route) and would gate catalog/auth too.
func RegisterRoutes(r chi.Router, h *Handler, tokens *auth.TokenIssuer) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticate(tokens), auth.RequireRole("CUSTOMER"))
		r.Get("/cart", h.GetCart)
		r.Post("/cart/items", h.AddItem)
		r.Post("/cart/merge", h.Merge)
		r.Patch("/cart/items/{cartItemId}", h.UpdateItem)
		r.Delete("/cart/items/{cartItemId}", h.DeleteItem)
	})
}
