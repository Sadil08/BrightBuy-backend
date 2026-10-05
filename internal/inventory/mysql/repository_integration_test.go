//go:build integration

// The line above is a "build tag". It means this file is ONLY compiled
// when you run tests with the "integration" tag, like this:
//
//	go test -tags=integration ./...
//
// A normal `go test ./...` skips this file, because these tests need a
// real database.

package mysql

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	// The underscore import loads the MySQL driver so database/sql can use it.
	// We never call it directly; it just registers itself.
	_ "github.com/go-sql-driver/mysql"

	"brightbuy-backend/internal/inventory/app"
)

// ---------------------------------------------------------------
// HELPER: open a connection to the test database
// ---------------------------------------------------------------
// Every test below calls this first to get a database connection.
func openIntegrationDB(t *testing.T) *sql.DB {
	// Marks this as a helper, so if it fails, Go shows the line in the
	// TEST that called it, not a line inside this function.
	t.Helper()

	// The connection string comes from an environment variable.
	dsn := os.Getenv("DB_DSN")
	if dsn == "" {
		// No database configured -> skip the test instead of failing it.
		t.Skip("DB_DSN is not set")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	// Close the connection automatically when the test finishes.
	t.Cleanup(func() {
		db.Close()
	})

	// sql.Open does not really connect. Ping does, so we use it to check
	// the database is reachable. We give it at most 5 seconds.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	return db
}

// ---------------------------------------------------------------
// TEST 1: Search by exact SKU
// ---------------------------------------------------------------
// Takes a real SKU from the database, searches for it, and checks
// that the result contains that SKU.
func TestRepositorySearchByExactSKU(t *testing.T) {
	db := openIntegrationDB(t)
	repository := NewRepository(db) // the REAL repository this time

	ctx := context.Background()

	// Get the SKU of the first product variant in the seeded data.
	// "Seeded" means test data that was inserted into the database earlier.
	var sku string
	if err := db.QueryRowContext(
		ctx,
		`SELECT sku
           FROM product_variant
          ORDER BY variant_id
          LIMIT 1`,
	).Scan(&sku); err != nil {
		t.Fatalf("find seeded SKU: %v", err)
	}

	// Search using that exact SKU (page 1, 20 results per page)
	items, total, err := repository.Search(
		ctx,
		sku,
		1,
		20,
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	// There must be at least one match
	if total < 1 {
		t.Fatalf("total = %d, want at least 1", total)
	}

	// Look through the results for the SKU we searched for
	found := false

	for _, item := range items {
		if item.SKU == sku {
			found = true

			// Stock can be 0, but never negative
			if item.StockQuantity < 0 {
				t.Fatalf(
					"stock quantity = %d, want non-negative",
					item.StockQuantity,
				)
			}
		}
	}

	if !found {
		t.Fatalf("search result did not contain SKU %q", sku)
	}
}

// ---------------------------------------------------------------
// TEST 2: Search by SKU prefix
// ---------------------------------------------------------------
// Searches with only the START of a SKU (the SKU minus its last
// character) and checks that matches are still found.
func TestRepositorySearchBySKUPrefix(t *testing.T) {
	db := openIntegrationDB(t)
	repository := NewRepository(db)

	ctx := context.Background()

	// Get a real SKU from the database
	var sku string
	if err := db.QueryRowContext(
		ctx,
		`SELECT sku
           FROM product_variant
          ORDER BY variant_id
          LIMIT 1`,
	).Scan(&sku); err != nil {
		t.Fatalf("find seeded SKU: %v", err)
	}

	// Remove the last character. Example: "BB-1001" becomes "BB-100"
	prefix := sku[:len(sku)-1]

	items, total, err := repository.Search(
		ctx,
		prefix,
		1,
		20,
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if total < 1 {
		t.Fatalf(
			"prefix %q returned total %d, want at least 1",
			prefix,
			total,
		)
	}

	if len(items) == 0 {
		t.Fatalf(
			"prefix %q returned no items",
			prefix,
		)
	}
}

// ---------------------------------------------------------------
// TEST 3: Search by product name
// ---------------------------------------------------------------
// Takes the name of a real product and checks that searching for
// that name returns results.
func TestRepositorySearchByProductName(t *testing.T) {
	db := openIntegrationDB(t)
	repository := NewRepository(db)

	ctx := context.Background()

	// Get the name of the first product in the seeded data
	var productName string
	if err := db.QueryRowContext(
		ctx,
		`SELECT name
           FROM product
          ORDER BY product_id
          LIMIT 1`,
	).Scan(&productName); err != nil {
		t.Fatalf("find seeded product: %v", err)
	}

	items, total, err := repository.Search(
		ctx,
		productName,
		1,
		20,
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if total < 1 {
		t.Fatalf(
			"product name %q returned total %d, want at least 1",
			productName,
			total,
		)
	}

	if len(items) == 0 {
		t.Fatalf(
			"product name %q returned no items",
			productName,
		)
	}
}

// ---------------------------------------------------------------
// TEST 4: Search with an empty query
// ---------------------------------------------------------------
// An empty query should not fail. It should return everything
// (one page of it), so we check we get at least one item back.
func TestRepositorySearchWithoutQuery(t *testing.T) {
	db := openIntegrationDB(t)
	repository := NewRepository(db)

	items, total, err := repository.Search(
		context.Background(),
		"", // empty query
		1,
		20,
	)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if total < 1 {
		t.Fatalf("total = %d, want at least 1", total)
	}

	if len(items) == 0 {
		t.Fatal("empty query returned no items")
	}
}

// ---------------------------------------------------------------
// TEST 5: Adjustment that would make stock negative is rejected
// ---------------------------------------------------------------
// Tries to remove MORE items than are in stock. The repository must:
//  1. return ErrAdjustmentBelowZero, and
//  2. leave the stock number unchanged in the database.
func TestRepositoryAdjustmentBelowZero(t *testing.T) {
	db := openIntegrationDB(t)
	repository := NewRepository(db)

	ctx := context.Background()

	// Find a variant that has some stock (stock_quantity > 0)
	var variantID int
	var stockQuantity int

	if err := db.QueryRowContext(
		ctx,
		`SELECT variant_id, stock_quantity
           FROM product_variant
          WHERE stock_quantity > 0
          ORDER BY variant_id
          LIMIT 1`,
	).Scan(&variantID, &stockQuantity); err != nil {
		t.Fatalf("find in-stock variant: %v", err)
	}

	// Find a user to act as the person making the adjustment
	var actingUserID int

	if err := db.QueryRowContext(
		ctx,
		`SELECT user_account_id
           FROM user_account
          ORDER BY user_account_id
          LIMIT 1`,
	).Scan(&actingUserID); err != nil {
		t.Fatalf("find acting user: %v", err)
	}

	// Try to remove (stock + 1) items. That would leave the stock at -1,
	// so it must be rejected.
	err := repository.Adjust(
		ctx,
		actingUserID,
		variantID,
		-(stockQuantity + 1), // negative number = remove stock
		"integration test rejection",
	)
	if err != app.ErrAdjustmentBelowZero {
		t.Fatalf(
			"got error %v, want ErrAdjustmentBelowZero",
			err,
		)
	}

	// Now read the stock again from the database
	var currentQuantity int

	if err := db.QueryRowContext(
		ctx,
		`SELECT stock_quantity
           FROM product_variant
          WHERE variant_id = ?`,
		variantID,
	).Scan(&currentQuantity); err != nil {
		t.Fatalf("read unchanged quantity: %v", err)
	}

	// It must be exactly the same as before. This proves the rejected
	// change was not saved (no half-finished update).
	if currentQuantity != stockQuantity {
		t.Fatalf(
			"quantity changed from %d to %d after rejected adjustment",
			stockQuantity,
			currentQuantity,
		)
	}
}
