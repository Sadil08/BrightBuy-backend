// This file is the HTTP layer: it receives web requests, reads the input,
// calls the cart service, and sends back JSON.

package httpapi // package name for this folder (the "httpapi" layer)

import (
	"encoding/json" // convert JSON - Go structs
	"net/http"      // HTTP server types: ResponseWriter, Request, status codes
	"strconv"       // convert strings to numbers (Atoi = string to int)

	cartapp "brightbuy-backend/internal/cart/app" // our cart service, given the short name "cartapp"

	"github.com/go-chi/chi/v5" // chi: the router that maps URLs to functions
)

// Handler holds everything the HTTP functions need.
type Handler struct {
	service *cartapp.Service // the business-logic layer we call
}

// NewHandler creates a Handler and gives it the service (dependency injection).
func NewHandler(service *cartapp.Service) *Handler {
	return &Handler{service: service} // return a pointer to the new Handler
}

// Shape of the JSON body for "add item": {"variant_id": 5, "quantity": 2}
type addItemRequest struct {
	VariantID int `json:"variant_id"`
	Quantity  int `json:"quantity"`
}

// Shape of the JSON body for "update item": {"quantity": 3}
// variantID is not here because it comes from the URL.
type updateItemRequest struct {
	Quantity int `json:"quantity"`
}

// GetCart handles: GET /cart?customer_id=1
func (h *Handler) GetCart(w http.ResponseWriter, r *http.Request) {

	// Read ?customer_id=... from the URL and convert it from string to int

	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {

		// not a number, or not positive
		http.Error(w, "customer_id must be a positive integer", http.StatusBadRequest)

		// send 400 + message
		return
	}

	// Ask the service for the cart; r.Context() carries cancellation/timeouts

	cart, err := h.service.GetCart(r.Context(), customerID)
	if err != nil {

		http.Error(w, err.Error(), http.StatusBadRequest) // send the error text with 400

		return
	}

	w.Header().Set("Content-Type", "application/json") // tell the client the reply is JSON

	// Convert the cart struct to JSON and write it into the response

	if err := json.NewEncoder(w).Encode(cart); err != nil {
		http.Error(w, "failed to encode cart", http.StatusInternalServerError) // 500: our fault
		return
	}
}

// AddItem handles: POST /cart/items?customer_id=1  with a JSON body

func (h *Handler) AddItem(w http.ResponseWriter, r *http.Request) {

	var req addItemRequest
	// Read the request body and fill req
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest) // body was not valid JSON
		return
	}

	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {
		http.Error(w, "customer_id must be a positive integer", http.StatusBadRequest)
		return
	}

	// Validate the body values (the service checks these again as a second safety net)
	if req.VariantID <= 0 || req.Quantity < 1 {
		http.Error(w, "variant_id must be positive and quantity must be at least 1", http.StatusBadRequest)
		return
	}

	// Call the service to add the item; it returns the updated cart
	cart, err := h.service.AddItem(r.Context(), customerID, req.VariantID, req.Quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")      // reply type is JSON
	if err := json.NewEncoder(w).Encode(cart); err != nil { // send the updated cart
		http.Error(w, "failed to encode cart", http.StatusInternalServerError)
		return
	}
}

// UpdateItem handles: PATCH /cart/items/{variantID}?customer_id=1  with a JSON body
func (h *Handler) UpdateItem(w http.ResponseWriter, r *http.Request) {
	var req updateItemRequest                                    // will hold the new quantity
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { // read JSON body
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// customer_id from the query string
	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {
		http.Error(w, "customer_id must be a positive integer", http.StatusBadRequest)
		return
	}

	// variantID comes from the URL path: /cart/items/7 -> "7"
	variantID, err := strconv.Atoi(chi.URLParam(r, "variantID"))
	if err != nil || variantID <= 0 {
		http.Error(w, "variantID must be a positive integer", http.StatusBadRequest)
		return
	}

	if req.Quantity < 1 { // quantity 0 or negative is not allowed on update
		http.Error(w, "quantity must be at least 1", http.StatusBadRequest)
		return
	}

	// The service checks the item exists, then updates the quantity
	cart, err := h.service.UpdateItemQuantity(r.Context(), customerID, variantID, req.Quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cart); err != nil { // send the updated cart
		http.Error(w, "failed to encode cart", http.StatusInternalServerError)
		return
	}
}

// DeleteItem handles: DELETE /cart/items/{variantID}?customer_id=1
// No JSON body is needed.
func (h *Handler) DeleteItem(w http.ResponseWriter, r *http.Request) {
	// customer_id from the query string
	customerID, err := strconv.Atoi(r.URL.Query().Get("customer_id"))
	if err != nil || customerID <= 0 {
		http.Error(w, "customer_id must be a positive integer", http.StatusBadRequest)
		return
	}

	// variantID from the URL path
	variantID, err := strconv.Atoi(chi.URLParam(r, "variantID"))
	if err != nil || variantID <= 0 {
		http.Error(w, "variantID must be a positive integer", http.StatusBadRequest)
		return
	}

	// Service deletes the item and returns the remaining cart
	cart, err := h.service.RemoveItem(r.Context(), customerID, variantID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cart); err != nil { // send the updated cart
		http.Error(w, "failed to encode cart", http.StatusInternalServerError)
		return
	}
}

// RegisterRoutes connects URL + HTTP method pairs to the handler functions.
func RegisterRoutes(r chi.Router, h *Handler) {
	r.Get("/cart", h.GetCart)                         // read the cart
	r.Post("/cart/items", h.AddItem)                  // add an item
	r.Patch("/cart/items/{variantID}", h.UpdateItem)  // change quantity ({variantID} is a URL parameter)
	r.Delete("/cart/items/{variantID}", h.DeleteItem) // remove an item
}
