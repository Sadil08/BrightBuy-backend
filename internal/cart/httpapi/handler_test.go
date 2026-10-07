package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCartRoutesRequireAuthenticatedCustomer(t *testing.T) {
	router := chi.NewRouter()
	RegisterRoutes(router, NewHandler(nil), []byte("01234567890123456789012345678901"))

	for _, path := range []string{"/cart", "/cart?customer_id=123"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Errorf("GET %s status = %d, want %d", path, response.Code, http.StatusUnauthorized)
		}
	}
}
