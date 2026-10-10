package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"brightbuy-backend/internal/ordering/app"
	orderdomain "brightbuy-backend/internal/ordering/domain"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"

	"github.com/go-chi/chi/v5"
)

type OrderStatusService interface {
	UpdateStatus(ctx context.Context, actingUserID, orderID int, newStatus string) error
	Cancel(ctx context.Context, actingUserID, orderID int) error
}

// StaffOrderQueries is the read side (list + detail) of the order manager's console.
type StaffOrderQueries interface {
	GetOrder(ctx context.Context, orderID int) (*orderdomain.Order, error)
	ListOrders(ctx context.Context, status string, page, size int) ([]orderdomain.Order, int, error)
}

type StaffHandler struct {
	statusService OrderStatusService
	queries       StaffOrderQueries
}

// WithQueries enables the read endpoints; kept separate so the write path stays independently testable.
func (h *StaffHandler) WithQueries(q StaffOrderQueries) *StaffHandler {
	h.queries = q
	return h
}

var orderStatuses = map[string]bool{"Placed": true, "Confirmed": true, "Processing": true, "Shipped": true, "ReadyForPickup": true, "Delivered": true, "Completed": true, "Cancelled": true}

type staffOrderView struct {
	orderdomain.Order
	NextStatuses []string `json:"nextStatuses"`
}

func (h *StaffHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && !orderStatuses[status] {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_STATUS", "unknown order status")
		return
	}
	page, size := positiveQueryInt(r, "page", 1), positiveQueryInt(r, "size", 20)
	if size > 100 {
		size = 100
	}
	orders, total, err := h.queries.ListOrders(r.Context(), status, page, size)
	if err != nil {
		slog.ErrorContext(r.Context(), "staff list orders failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not list orders")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, orderListResponse{Items: orders, Page: pageResponse{Page: page, Size: size, Total: total}})
}

func (h *StaffHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	orderID, err := strconv.Atoi(chi.URLParam(r, "orderId"))
	if err != nil || orderID <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
		return
	}
	order, err := h.queries.GetOrder(r.Context(), orderID)
	if errors.Is(err, app.ErrOrderNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "staff get order failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not retrieve order")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, staffOrderView{Order: *order, NextStatuses: app.NextStatuses(string(order.Status))})
}

func NewStaffHandler(statusService OrderStatusService) *StaffHandler {
	return &StaffHandler{statusService: statusService}
}

type orderStatusUpdateRequest struct {
	Status string `json:"status"`
}

func (h *StaffHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || claims.UserID <= 0 {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return
	}

	orderID, err := strconv.Atoi(chi.URLParam(r, "orderId"))
	if err != nil || orderID <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
		return
	}

	var req orderStatusUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return
	}

	// Check permissions dynamically based on the requested transition
	if req.Status == "Cancelled" {
		if !claims.HasPermission("order:cancel") {
			httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "missing order:cancel permission")
			return
		}
	} else {
		if !claims.HasPermission("order:status:update") {
			httpx.WriteError(w, http.StatusForbidden, "FORBIDDEN", "missing order:status:update permission")
			return
		}
	}

	err = h.statusService.UpdateStatus(r.Context(), claims.UserID, orderID, req.Status)
	if err != nil {
		var invalidErr *app.InvalidTransitionError
		switch {
		case errors.As(err, &invalidErr):
			httpx.WriteError(w, http.StatusConflict, "INVALID_TRANSITION", err.Error())
		case errors.Is(err, app.ErrOrderNotFound):
			httpx.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
		case errors.Is(err, app.ErrInvalidTransition): // from repo
			httpx.WriteError(w, http.StatusConflict, "INVALID_TRANSITION", "invalid order status transition")
		default:
			slog.ErrorContext(r.Context(), "update order status failed", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not update order status")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RegisterStaffRoutes wires staff order management endpoints
func RegisterStaffRoutes(r chi.Router, h *StaffHandler, tokens *auth.TokenIssuer) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticate(tokens))
		// Permissions are checked inside the handler because they depend on the request body.
		r.Patch("/staff/orders/{orderId}/status", h.UpdateStatus)
		if h.queries != nil {
			r.With(auth.RequirePermission("order:status:update")).Get("/staff/orders", h.ListOrders)
			r.With(auth.RequirePermission("order:status:update")).Get("/staff/orders/{orderId}", h.GetOrder)
		}
	})
}
