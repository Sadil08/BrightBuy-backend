package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/inventory/app"
	"brightbuy-backend/internal/inventory/domain"
	"brightbuy-backend/internal/shared/httpx"
)

type Service interface {
	Search(
		ctx context.Context,
		query string,
		page int,
		size int,
	) ([]domain.VariantStock, int, error)

	Adjust(
		ctx context.Context,
		actingUserID int,
		variantID int,
		delta int,
		reason string,
	) error
}

type ActorID func(context.Context) (int, bool)

type PermissionMiddleware func(http.Handler) http.Handler

type Handler struct {
	service Service
	actorID ActorID
}

func NewHandler(service Service, actorID ActorID) *Handler {
	return &Handler{
		service: service,
		actorID: actorID,
	}
}

func denyStockPermission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		httpx.WriteError(
			w,
			http.StatusForbidden,
			"FORBIDDEN",
			"missing permission: stock:adjust",
		)
	})
}

func RegisterRoutes(
	r chi.Router,
	handler *Handler,
	requirePermission PermissionMiddleware,
) {
	r.With(requirePermission).Get(
		"/api/v1/staff/variants",
		handler.Search,
	)

	r.With(requirePermission).Post(
		"/api/v1/staff/variants/{variantId}/stock",
		handler.Adjust,
	)
}

type searchResponse struct {
	Items []variantResponse `json:"items"`
	Page  pageResponse      `json:"page"`
}

type variantResponse struct {
	VariantID     int    `json:"variantId"`
	SKU           string `json:"sku"`
	ProductName   string `json:"productName"`
	StockQuantity int    `json:"stockQuantity"`
}

type pageResponse struct {
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
}

type adjustmentRequest struct {
	Delta  int    `json:"delta"`
	Reason string `json:"reason"`
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) {
	page, size, err := parsePagination(r)
	if err != nil {
		httpx.WriteError(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"invalid pagination",
		)
		return
	}

	items, total, err := h.service.Search(
		r.Context(),
		r.URL.Query().Get("q"),
		page,
		size,
	)
	if err != nil {
		if errors.Is(err, app.ErrInvalidRequest) {
			httpx.WriteError(
				w,
				http.StatusBadRequest,
				"INVALID_REQUEST",
				"invalid inventory request",
			)
			return
		}

		httpx.WriteError(
			w,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to search inventory",
		)
		return
	}

	responseItems := make([]variantResponse, 0, len(items))

	for _, item := range items {
		responseItems = append(responseItems, variantResponse{
			VariantID:     item.VariantID,
			SKU:           item.SKU,
			ProductName:   item.ProductName,
			StockQuantity: item.StockQuantity,
		})
	}

	httpx.WriteJSON(
		w,
		http.StatusOK,
		searchResponse{
			Items: responseItems,
			Page: pageResponse{
				Page:  page,
				Size:  size,
				Total: total,
			},
		},
	)
}

func (h *Handler) Adjust(w http.ResponseWriter, r *http.Request) {
	if h.actorID == nil {
		httpx.WriteError(
			w,
			http.StatusUnauthorized,
			"UNAUTHORIZED",
			"authentication required",
		)
		return
	}

	actingUserID, ok := h.actorID(r.Context())
	if !ok || actingUserID <= 0 {
		httpx.WriteError(
			w,
			http.StatusUnauthorized,
			"UNAUTHORIZED",
			"authentication required",
		)
		return
	}

	variantID, err := strconv.Atoi(
		chi.URLParam(r, "variantId"),
	)
	if err != nil || variantID <= 0 {
		httpx.WriteError(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"invalid variant ID",
		)
		return
	}

	var request adjustmentRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1<<16) // a {delta, reason} body is tiny
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		httpx.WriteError(
			w,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"invalid JSON request",
		)
		return
	}

	err = h.service.Adjust(
		r.Context(),
		actingUserID,
		variantID,
		request.Delta,
		request.Reason,
	)
	if err != nil {
		switch {
		case errors.Is(err, app.ErrInvalidRequest):
			httpx.WriteError(
				w,
				http.StatusBadRequest,
				"INVALID_REQUEST",
				"invalid stock adjustment",
			)

		case errors.Is(err, app.ErrAdjustmentBelowZero):
			httpx.WriteError(
				w,
				http.StatusConflict,
				"ADJUSTMENT_BELOW_ZERO",
				"adjustment would take stock below zero",
			)

		case errors.Is(err, app.ErrVariantNotFound):
			httpx.WriteError(
				w,
				http.StatusNotFound,
				"VARIANT_NOT_FOUND",
				"variant not found",
			)

		default:
			httpx.WriteError(
				w,
				http.StatusInternalServerError,
				"INTERNAL_ERROR",
				"unable to adjust stock",
			)
		}

		return
	}

	httpx.WriteJSON(
		w,
		http.StatusOK,
		map[string]string{
			"status": "updated",
		},
	)
}

func parsePagination(r *http.Request) (int, int, error) {
	page := 1
	size := 20

	if value := r.URL.Query().Get("page"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			return 0, 0, app.ErrInvalidRequest
		}
		page = parsed
	}

	if value := r.URL.Query().Get("size"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			return 0, 0, app.ErrInvalidRequest
		}
		size = parsed
	}

	return page, size, nil
}

/*Implements GET /api/v1/staff/variants.
Implements POST /api/v1/staff/variants/{variantId}/stock.
Keeps exact stock quantities inside the protected inventory API.
Gets the acting user from a function supplied by the auth layer.
Does not trust a user ID from the request body.
Maps below-zero adjustments to HTTP 409.
Maps missing variants to HTTP 404.
Maps unauthenticated requests to HTTP 401*/
