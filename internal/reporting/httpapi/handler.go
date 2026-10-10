package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"brightbuy-backend/internal/reporting/app"
	"brightbuy-backend/internal/shared/httpx"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	service *app.Service
}

const (
	basisSales    = "order_date; Cancelled orders excluded; sales value excludes tax and delivery fee"
	basisCategory = "order_date; Cancelled orders excluded; an order is counted once per category it contains"
	basisDelivery = "unfulfilled orders; estimated_date is today or later"
	basisCustomer = "customer order history; payment status is reported per order; sales value excludes tax and delivery fee"
)

func NewHandler(service *app.Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes mounts the reports under /api/v1/reports. guard authenticates and authorises the
// caller (reports:view) — it is injected so main.go uses the shared auth middleware, not a private copy.
func RegisterRoutes(r chi.Router, handler *Handler, guard func(http.Handler) http.Handler) {
	r.Route("/api/v1/reports", func(r chi.Router) {
		r.Use(guard)
		r.Get("/quarterly-sales", handler.quarterlySales)
		r.Get("/top-selling-products", handler.topSellingProducts)
		r.Get("/category-wise-orders", handler.categoryWiseOrders)
		r.Get("/upcoming-deliveries", handler.upcomingDeliveries)
		r.Get("/customer-order-summary", handler.customerOrderSummary)
	})
}

func (h *Handler) quarterlySales(w http.ResponseWriter, r *http.Request) {
	year, err := queryInt(r, "year")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_YEAR", err.Error())
		return
	}
	rows, err := h.service.QuarterlySales(r.Context(), year)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REPORT_REQUEST", err.Error())
		return
	}
	setBasis(w, basisSales)
	if wantsCSV(r) {
		csvRows := make([][]string, 0, len(rows))
		for _, row := range rows {
			csvRows = append(csvRows, []string{strconv.Itoa(row.Year), strconv.Itoa(row.Quarter), row.SalesValue})
		}
		if err := httpx.WriteCSV(w, "quarterly-sales.csv", []string{"year", "quarter", "salesValue"}, csvRows); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "CSV_WRITE_FAILED", "could not write report CSV")
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) topSellingProducts(w http.ResponseWriter, r *http.Request) {
	from, to, err := optionalDateRange(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_DATE_RANGE", err.Error())
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_LIMIT", "limit must be an integer")
			return
		}
	}
	report, err := h.service.TopSellingProducts(r.Context(), from, to, limit)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REPORT_REQUEST", err.Error())
		return
	}
	setBasis(w, basisSales)
	setDateRangeHeaders(w, report.From, report.To)
	if wantsCSV(r) {
		csvRows := make([][]string, 0, len(report.Products))
		for _, row := range report.Products {
			csvRows = append(csvRows, []string{strconv.FormatInt(row.ProductID, 10), row.ProductName, strconv.FormatInt(row.Quantity, 10), row.Revenue})
		}
		if err := httpx.WriteCSV(w, "top-selling-products.csv", []string{"productId", "productName", "quantitySold", "revenue"}, csvRows); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "CSV_WRITE_FAILED", "could not write report CSV")
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) categoryWiseOrders(w http.ResponseWriter, r *http.Request) {
	from, to, err := optionalDateRange(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_DATE_RANGE", err.Error())
		return
	}
	report, err := h.service.CategoryWiseOrders(r.Context(), from, to)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REPORT_REQUEST", err.Error())
		return
	}
	setBasis(w, basisCategory)
	if wantsCSV(r) {
		csvRows := make([][]string, 0, len(report.Categories))
		for _, row := range report.Categories {
			csvRows = append(csvRows, []string{strconv.FormatInt(row.CategoryID, 10), row.Name, strconv.FormatInt(row.OrderCount, 10), report.Note})
		}
		if err := httpx.WriteCSV(w, "category-wise-orders.csv", []string{"categoryId", "name", "orderCount", "note"}, csvRows); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "CSV_WRITE_FAILED", "could not write report CSV")
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, report)
}

func (h *Handler) upcomingDeliveries(w http.ResponseWriter, r *http.Request) {
	rows, err := h.service.UpcomingDeliveries(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "REPORT_QUERY_FAILED", "could not load the report")
		return
	}
	setBasis(w, basisDelivery)
	if wantsCSV(r) {
		csvRows := make([][]string, 0, len(rows))
		for _, row := range rows {
			csvRows = append(csvRows, []string{strconv.FormatInt(row.OrderID, 10), row.CustomerName, row.CustomerEmail, row.DeliveryMode, row.Address, row.EstimatedDate.Format("2006-01-02"), row.OrderStatus})
		}
		if err := httpx.WriteCSV(w, "upcoming-deliveries.csv", []string{"orderId", "customerName", "customerEmail", "deliveryMode", "address", "estimatedDate", "orderStatus"}, csvRows); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "CSV_WRITE_FAILED", "could not write report CSV")
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rows)
}

func (h *Handler) customerOrderSummary(w http.ResponseWriter, r *http.Request) {
	from, to, err := optionalDateRange(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_DATE_RANGE", err.Error())
		return
	}
	var customerID *int64
	if raw := r.URL.Query().Get("customerId"); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value <= 0 {
			httpx.WriteError(w, http.StatusBadRequest, "INVALID_CUSTOMER_ID", "customerId must be a positive integer")
			return
		}
		customerID = &value
	}
	rows, err := h.service.CustomerOrderPayments(r.Context(), from, to, customerID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_REPORT_REQUEST", err.Error())
		return
	}
	setBasis(w, basisCustomer)
	if wantsCSV(r) {
		csvRows := make([][]string, 0, len(rows))
		for _, row := range rows {
			csvRows = append(csvRows, []string{strconv.FormatInt(row.CustomerID, 10), row.CustomerName, row.CustomerEmail, strconv.FormatInt(row.OrderID, 10), row.OrderDate.Format(time.RFC3339), row.OrderStatus, row.SalesValue, row.PaymentMethod, row.PaymentStatus, row.PaymentAmount})
		}
		if err := httpx.WriteCSV(w, "customer-order-summary.csv", []string{"customerId", "customerName", "customerEmail", "orderId", "orderDate", "orderStatus", "salesValue", "paymentMethod", "paymentStatus", "paymentAmount"}, csvRows); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "CSV_WRITE_FAILED", "could not write report CSV")
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, rows)
}

func queryInt(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, &queryError{message: name + " is required"}
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &queryError{message: name + " must be an integer"}
	}
	return value, nil
}

func optionalDateRange(r *http.Request) (*time.Time, *time.Time, error) {
	fromRaw := r.URL.Query().Get("from")
	toRaw := r.URL.Query().Get("to")
	if fromRaw == "" && toRaw == "" {
		return nil, nil, nil
	}
	if fromRaw == "" || toRaw == "" {
		return nil, nil, &queryError{message: "from and to must be provided together"}
	}
	from, err := time.Parse("2006-01-02", fromRaw)
	if err != nil {
		return nil, nil, &queryError{message: "from must use YYYY-MM-DD"}
	}
	toDate, err := time.Parse("2006-01-02", toRaw)
	if err != nil {
		return nil, nil, &queryError{message: "to must use YYYY-MM-DD"}
	}
	to := toDate.AddDate(0, 0, 1)
	return &from, &to, nil
}

func wantsCSV(r *http.Request) bool {
	if strings.EqualFold(r.URL.Query().Get("format"), "csv") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/csv")
}

func setBasis(w http.ResponseWriter, basis string) {
	w.Header().Set("X-Report-Basis", basis)
}

func setDateRangeHeaders(w http.ResponseWriter, from, to *string) {
	if from == nil {
		w.Header().Set("X-Report-Range", "all-time")
		return
	}
	w.Header().Set("X-Report-From", *from)
	if to != nil {
		w.Header().Set("X-Report-To", *to)
	}
}

type queryError struct {
	message string
}

func (e *queryError) Error() string { return e.message }
