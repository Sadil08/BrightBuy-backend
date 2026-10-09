//go:build integration

package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"brightbuy-backend/internal/delivery/app"
	"brightbuy-backend/internal/delivery/httpapi"
	"brightbuy-backend/internal/shared/testdb"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	db, cleanup, err := testdb.Start(context.Background(), "brightbuy_delivery_test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testDB = db
	code := m.Run()
	cleanup()
	os.Exit(code)
}

// preview calls the REAL endpoint stack (handler -> service -> repository -> fn_estimate_delivery_days).
func preview(t *testing.T, body string) (int, map[string]any) {
	t.Helper()
	router := chi.NewRouter()
	httpapi.RegisterRoutes(router, httpapi.NewHandler(app.NewService(NewRepository(testDB), NewRepository(testDB), time.Now)))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/delivery/estimate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func days(t *testing.T, body string) int {
	t.Helper()
	code, out := preview(t, body)
	if code != http.StatusOK {
		t.Fatalf("status %d body %v for %s", code, out, body)
	}
	return int(out["estimatedDays"].(float64))
}

func TestDeliveryEstimateAcceptanceCriteria(t *testing.T) {
	mainCity := testdb.City(t, testDB, "AC Main", "Main")
	otherCity := testdb.City(t, testDB, "AC Other", "Other")
	inStock := testdb.Variant(t, testDB, "AC In Stock", "AC-IN", "10.00", 5)
	outA := testdb.Variant(t, testDB, "AC Out A", "AC-OUT-A", "10.00", 0)
	outB := testdb.Variant(t, testDB, "AC Out B", "AC-OUT-B", "10.00", 0)

	item := func(id int) string { return fmt.Sprintf(`{"variantId":%d,"quantity":1}`, id) }
	body := func(mode string, city int, items ...string) string {
		c := ""
		if city != 0 {
			c = fmt.Sprintf(`"cityId":%d,`, city)
		}
		return fmt.Sprintf(`{"mode":%q,%s"items":[%s]}`, mode, c, strings.Join(items, ","))
	}

	cases := []struct {
		name string
		body string
		want int
	}{
		{"AC-1 Standard, Main City, all in stock = 5", body("StandardDelivery", mainCity, item(inStock)), 5},
		{"AC-2 Standard, Other City, all in stock = 7", body("StandardDelivery", otherCity, item(inStock)), 7},
		{"AC-3 +3 once for one out-of-stock line", body("StandardDelivery", mainCity, item(inStock), item(outA)), 8},
		{"AC-3 +3 ONCE not per out-of-stock line", body("StandardDelivery", mainCity, item(outA), item(outB)), 8},
		{"AC-3 Other City + out of stock = 10", body("StandardDelivery", otherCity, item(outA)), 10},
		{"AC-4 Store Pickup in stock = 0 (Main city ignored)", body("StorePickup", mainCity, item(inStock)), 0},
		{"AC-4 Store Pickup in stock = 0 (Other city ignored)", body("StorePickup", otherCity, item(inStock)), 0},
		{"AC-4 Store Pickup in stock = 0 (no city)", body("StorePickup", 0, item(inStock)), 0},
		{"AC-5 Store Pickup out of stock = 3 (TBD-3)", body("StorePickup", 0, item(outA)), 3},
		{"AC-5 Store Pickup out of stock = 3 regardless of city", body("StorePickup", otherCity, item(outA)), 3},
		{"unknown city degrades to Other = 7", body("StandardDelivery", 999999, item(inStock)), 7},
		{"empty items = base estimate only", body("StandardDelivery", mainCity), 5},
	}
	for _, c := range cases {
		if got := days(t, c.body); got != c.want {
			t.Errorf("%s: got %d days, want %d", c.name, got, c.want)
		}
	}
}

func TestEstimatedDateIsCalendarDaysFromToday(t *testing.T) { // FR-DELIVERY-4
	city := testdb.City(t, testDB, "Date City", "Main")
	v := testdb.Variant(t, testDB, "Date Item", "DATE-1", "1.00", 5)
	_, out := preview(t, fmt.Sprintf(`{"mode":"StandardDelivery","cityId":%d,"items":[{"variantId":%d,"quantity":1}]}`, city, v))
	want := time.Now().UTC().AddDate(0, 0, 5).Format("2006-01-02")
	if out["estimatedDate"] != want {
		t.Errorf("estimatedDate = %v, want %s", out["estimatedDate"], want)
	}
}

// AC-DELIVERY-6: the preview and the estimate checkout stores must be identical — the whole point of
// calling the same SQL function instead of reimplementing the rule in Go.
func TestPreviewMatchesWhatCheckoutStores(t *testing.T) {
	_, customerID := testdb.Customer(t, testDB, "ac6@example.test")
	city := testdb.City(t, testDB, "AC6 City", "Other")
	inStock := testdb.Variant(t, testDB, "AC6 In", "AC6-IN", "5.00", 10)
	scarce := testdb.Variant(t, testDB, "AC6 Scarce", "AC6-SCARCE", "5.00", 1)

	for i, tc := range []struct {
		mode  string
		city  int
		items string
	}{
		{"StandardDelivery", city, fmt.Sprintf(`[{"variant_id":%d,"quantity":2}]`, inStock)},
		{"StorePickup", 0, fmt.Sprintf(`[{"variant_id":%d,"quantity":1}]`, inStock)},
		// ordering the last unit leaves the variant at 0, but the estimate is computed BEFORE the
		// decrement inside the procedure, exactly as the preview sees it.
		{"StandardDelivery", city, fmt.Sprintf(`[{"variant_id":%d,"quantity":1}]`, scarce)},
	} {
		previewBody := fmt.Sprintf(`{"mode":%q,"items":%s}`, tc.mode, strings.ReplaceAll(strings.ReplaceAll(tc.items, "variant_id", "variantId"), `"quantity"`, `"quantity"`))
		if tc.city != 0 {
			previewBody = fmt.Sprintf(`{"mode":%q,"cityId":%d,"items":%s}`, tc.mode, tc.city, strings.ReplaceAll(tc.items, "variant_id", "variantId"))
		}
		code, out := preview(t, previewBody)
		if code != http.StatusOK {
			t.Fatalf("case %d preview: %d %v", i, code, out)
		}
		orderID := testdb.PlaceOrder(t, testDB, customerID, tc.items, tc.mode, tc.city, fmt.Sprintf("ac6-%d", i))

		var storedDays int
		var storedDate string
		if err := testDB.QueryRow(`SELECT estimated_days, DATE_FORMAT(estimated_date,'%Y-%m-%d') FROM delivery WHERE order_id = ?`, orderID).Scan(&storedDays, &storedDate); err != nil {
			t.Fatal(err)
		}
		if int(out["estimatedDays"].(float64)) != storedDays || out["estimatedDate"] != storedDate {
			t.Errorf("case %d: preview = %v days / %v, checkout stored %d days / %s", i, out["estimatedDays"], out["estimatedDate"], storedDays, storedDate)
		}
	}
}

func TestInvalidRequestsAreRejected(t *testing.T) {
	city := testdb.City(t, testDB, "Invalid City", "Main")
	tooMany := `{"mode":"StorePickup","items":[` + strings.TrimSuffix(strings.Repeat(`{"variantId":1,"quantity":1},`, 101), ",") + `]}`
	for name, body := range map[string]string{
		"unknown mode":            `{"mode":"Drone","items":[]}`,
		"Standard without a city": `{"mode":"StandardDelivery","items":[]}`,
		"zero quantity":           fmt.Sprintf(`{"mode":"StandardDelivery","cityId":%d,"items":[{"variantId":1,"quantity":0}]}`, city),
		"unknown field":           `{"mode":"StorePickup","items":[],"price":"0.01"}`,
		"malformed JSON":          `{`,
		"more than 100 items":     tooMany,
	} {
		if code, _ := preview(t, body); code != http.StatusBadRequest {
			t.Errorf("%s: got %d, want 400", name, code)
		}
	}
}

func TestListCitiesIsAlphabeticalAndHidesClassification(t *testing.T) {
	testdb.City(t, testDB, "Zzz Town", "Other")
	testdb.City(t, testDB, "Aaa Town", "Main")

	router := chi.NewRouter()
	httpapi.RegisterRoutes(router, httpapi.NewHandler(app.NewService(NewRepository(testDB), NewRepository(testDB), time.Now)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/cities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var cities []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cities); err != nil {
		t.Fatal(err)
	}
	if len(cities) < 8 { // 6 seeded by migration 0010 + the two above
		t.Fatalf("got %d cities, want at least 8 (seed + fixtures)", len(cities))
	}
	prev := ""
	for _, c := range cities {
		name := strings.ToLower(c["name"].(string)) // the column's collation sorts case-insensitively
		if name < prev {
			t.Fatalf("not alphabetical: %q after %q", name, prev)
		}
		prev = name
		if _, leaked := c["classification"]; leaked {
			t.Fatal("classification must not be exposed")
		}
	}
}
