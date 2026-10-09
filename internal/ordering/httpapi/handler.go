package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"brightbuy-backend/internal/ordering/app"
	orderdomain "brightbuy-backend/internal/ordering/domain"
	"brightbuy-backend/internal/shared/auth"
	"brightbuy-backend/internal/shared/httpx"
	"github.com/go-chi/chi/v5"
)

type Service interface {
	PlaceOrder(context.Context, int, string, app.PlaceOrderRequest) (*orderdomain.Order, error)
	GetOrder(context.Context, int, int) (*orderdomain.Order, error)
	ListOrders(context.Context, int, int, int) ([]orderdomain.Order, int, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

type checkoutRequest struct {
	DeliveryMode    orderdomain.DeliveryMode  `json:"deliveryMode"`
	DeliveryCityID  *int                      `json:"deliveryCityId"`
	DeliveryAddress string                    `json:"deliveryAddress"`
	PaymentMethod   orderdomain.PaymentMethod `json:"paymentMethod"`
	Card            struct {
		Token string `json:"token"`
	} `json:"card"`
}

type pageResponse struct {
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
}

type orderListResponse struct {
	Items []orderdomain.Order `json:"items"`
	Page  pageResponse        `json:"page"`
}

type stockErrorResponse struct {
	Code             string                        `json:"code"`
	Message          string                        `json:"message"`
	UnavailableLines []orderdomain.UnavailableLine `json:"unavailableLines"`
}

func (h *Handler) Checkout(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerID(w, r)
	if !ok {
		return
	}
	var req checkoutRequest
	if err := decodeJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	order, err := h.service.PlaceOrder(r.Context(), customerID, key, app.PlaceOrderRequest{
		DeliveryMode: req.DeliveryMode, DeliveryCityID: req.DeliveryCityID,
		DeliveryAddress: req.DeliveryAddress, PaymentMethod: req.PaymentMethod,
		CardToken: req.Card.Token,
	})
	if err != nil {
		var stockErr *app.StockExceededError
		switch {
		case errors.As(err, &stockErr):
			httpx.WriteJSON(w, http.StatusConflict, stockErrorResponse{
				Code: "STOCK_EXCEEDED", Message: app.ErrStockExceeded.Error(),
				UnavailableLines: stockErr.Lines,
			})
		case errors.Is(err, app.ErrPaymentFailed):
			httpx.WriteError(w, http.StatusConflict, "PAYMENT_FAILED", "payment authorization failed")
		case errors.Is(err, app.ErrEmptyCart):
			httpx.WriteError(w, http.StatusBadRequest, "EMPTY_CART", "cart is empty")
		case errors.Is(err, app.ErrInvalidRequest):
			httpx.WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", "checkout request is invalid")
		default:
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "checkout failed")
		}
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, order)
}

func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerID(w, r)
	if !ok {
		return
	}
	page, size := positiveQueryInt(r, "page", 1), positiveQueryInt(r, "size", 20)
	orders, total, err := h.service.ListOrders(r.Context(), customerID, page, size)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not list orders")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, orderListResponse{
		Items: orders, Page: pageResponse{Page: page, Size: size, Total: total},
	})
}

func (h *Handler) GetOrder(w http.ResponseWriter, r *http.Request) {
	customerID, ok := customerID(w, r)
	if !ok {
		return
	}
	orderID, err := strconv.Atoi(chi.URLParam(r, "orderId"))
	if err != nil || orderID <= 0 {
		httpx.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
		return
	}
	order, err := h.service.GetOrder(r.Context(), customerID, orderID)
	if errors.Is(err, app.ErrOrderNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "ORDER_NOT_FOUND", "order not found")
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not retrieve order")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, order)
}

// RegisterRoutes wires checkout and order history. Every route needs a valid session and the
// CUSTOMER role (FR-CHECKOUT-20); Group scopes that middleware to these routes only.
func RegisterRoutes(r chi.Router, h *Handler, tokens *auth.TokenIssuer) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Authenticate(tokens), auth.RequireRole("CUSTOMER"))
		r.Post("/checkout", h.Checkout)
		r.Get("/orders", h.ListOrders)
		r.Get("/orders/{orderId}", h.GetOrder)
	})
}

// customerID returns the logged-in customer's id from the verified JWT claims — never from the
// request (FR-CHECKOUT-19, SEC-CHECKOUT-2). A token without one (issued before the id was added to
// the JWT, at most 15 minutes old) is told to sign in again rather than guessed at.
func customerID(w http.ResponseWriter, r *http.Request) (int, bool) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil || claims.CustomerID <= 0 {
		httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required")
		return 0, false
	}
	return claims.CustomerID, true
}

func positiveQueryInt(r *http.Request, key string, fallback int) int {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func decodeJSON(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		return err
	}
	if len(body) > 1<<20 {
		return errors.New("request body too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}
