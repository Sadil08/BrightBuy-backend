package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/delivery/app"
	"brightbuy-backend/internal/delivery/domain"
	"brightbuy-backend/internal/shared/httpx"
)

type Service interface {
	ListCities(ctx context.Context) ([]domain.City, error)

	Preview(
		ctx context.Context,
		mode domain.DeliveryMode,
		cityID *int,
		items []domain.LineInput,
	) (domain.Estimate, error)
}

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

type estimateRequest struct {
	Mode   domain.DeliveryMode `json:"mode"`
	CityID *int                `json:"cityId"`
	Items  []itemRequest       `json:"items"`
}

type itemRequest struct {
	VariantID int `json:"variantId"`
	Quantity  int `json:"quantity"`
}

type estimateResponse struct {
	Mode          domain.DeliveryMode `json:"mode"`
	EstimatedDate string              `json:"estimatedDate"`
	EstimatedDays int                 `json:"estimatedDays"`
}

func RegisterRoutes(r chi.Router, handler *Handler) {
	r.Post("/api/v1/delivery/estimate", handler.Preview)
	r.Get("/api/v1/cities", handler.ListCities)
}

type cityResponse struct {
	CityID int    `json:"cityId"`
	Name   string `json:"name"`
}

// ListCities handles GET /api/v1/cities — public reference data for the checkout city picker.
func (h *Handler) ListCities(w http.ResponseWriter, r *http.Request) {
	cities, err := h.service.ListCities(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "list cities failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to list cities")
		return
	}

	out := make([]cityResponse, 0, len(cities))
	for _, c := range cities {
		out = append(out, cityResponse{CityID: c.ID, Name: c.Name})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	var request estimateRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1<<18) // public endpoint: cap the body (100 items ~ 5 KB)
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

	items := make([]domain.LineInput, 0, len(request.Items))
	for _, item := range request.Items {
		items = append(items, domain.LineInput{
			VariantID: item.VariantID,
			Quantity:  item.Quantity,
		})
	}

	result, err := h.service.Preview(
		r.Context(),
		request.Mode,
		request.CityID,
		items,
	)
	if err != nil {
		if errors.Is(err, app.ErrInvalidRequest) {
			httpx.WriteError(
				w,
				http.StatusBadRequest,
				"INVALID_REQUEST",
				"invalid delivery estimate request",
			)
			return
		}

		httpx.WriteError(
			w,
			http.StatusInternalServerError,
			"INTERNAL_ERROR",
			"unable to calculate delivery estimate",
		)
		return
	}

	response := estimateResponse{
		Mode:          result.Mode,
		EstimatedDate: result.EstimatedDate.UTC().Format("2006-01-02"),
		EstimatedDays: result.EstimatedDays,
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

/*This handler acts as the bridge between the HTTP API and the delivery
application service. It receives POST /api/v1/delivery/estimate requests,
validates and converts the JSON data, passes it to Service.Preview() to
calculate the delivery estimate, handles any errors, and returns the
estimated delivery mode, number of days, and date as a JSON response.*/
