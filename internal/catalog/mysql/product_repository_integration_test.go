//go:build integration

// This file only compiles/runs with `go test -tags=integration` (see .github/workflows/ci.yml,
// which already has the integration-test step scaffolded, gated behind exactly this tag). Unit
// tests (internal/catalog/app's fake-repository tests) run on every `go test`; these need a real
// MySQL, which is slow to start and needs Docker — not something you want on every save.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql" // registers the "mysql://" scheme migrate.New understands
	_ "github.com/golang-migrate/migrate/v4/source/file"    // registers the "file://" scheme migrate.New understands
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql" // aliased: this package is also named "mysql"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/dbx"
)

// testDB is shared by every test in this file — starting a MySQL container takes several seconds,
// so TestMain pays that cost ONCE for the whole package, not once per test function.
var testDB *sql.DB

// TestMain replaces the default test runner for this package: it runs setup, then m.Run() (which
// runs every Test* function), then cleanup — instead of each test managing its own container.
//
// Note the shape here: setup/teardown logic lives in runTestMain, and TestMain itself only calls
// os.Exit(runTestMain(m)). That split matters because deferred functions never run after os.Exit —
// os.Exit terminates the process immediately, defers or not. Putting `defer terminate()` directly in
// TestMain above an os.Exit call would silently skip that cleanup; returning an exit code from an
// inner function first, THEN calling os.Exit exactly once at the very end, is what makes defer safe
// to use in between.
func TestMain(m *testing.M) {
	os.Exit(runTestMain(m))
}

func runTestMain(m *testing.M) int {
	ctx := context.Background()

	container, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("brightbuy_test"),
		tcmysql.WithUsername("brightbuy_test"),
		tcmysql.WithPassword("test-password"),
		// Matches docker-compose.yml's `command: --log-bin-trust-function-creators=1`: MySQL
		// refuses to CREATE FUNCTION at all with binary logging on unless either the caller has the
		// SUPER privilege or this flag is set — even for a function marked DETERMINISTIC, because
		// that declaration is just a hint MySQL doesn't fully trust on its own. Without this,
		// fn_is_variant_in_stock's own migration fails here exactly like it would against a default
		// MySQL 8 server anywhere else.
		testcontainers.WithCmd("--log-bin-trust-function-creators=1"),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start mysql container:", err)
		return 1
	}
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintln(os.Stderr, "terminate mysql container:", err)
		}
	}()

	dsn, err := container.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		fmt.Fprintln(os.Stderr, "build connection string:", err)
		return 1
	}

	// Apply the real db/migrations/*.up.sql files using the golang-migrate LIBRARY — the exact same
	// engine `migrate -path db/migrations -database $DSN up` uses in dev/prod
	// (specs/global/12_DEVOPS_CICD.md §3). This deliberately does NOT use testcontainers' MySQL
	// module's WithScripts option: that copies files into /docker-entrypoint-initdb.d/, which the
	// mysql image runs through the `mysql` COMMAND-LINE CLIENT — and that client, unlike
	// golang-migrate's direct database/sql connection, really does split its input on every `;`,
	// which breaks a CREATE FUNCTION body's internal semicolons. Confirmed by hitting exactly that
	// failure while building this test: same file, two different runners, two different semicolon
	// rules. Going through golang-migrate here is what makes this test prove the thing that
	// actually matters — "does the migration our own tooling runs work" — rather than "does this
	// file happen to also survive a runner nothing in this project's pipeline uses."
	migrationsDir, err := migrationsDirPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "locate migrations dir:", err)
		return 1
	}
	m2, err := migrate.New("file://"+migrationsDir, "mysql://"+dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create migrate instance:", err)
		return 1
	}
	if err := m2.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintln(os.Stderr, "run migrations:", err)
		return 1
	}

	db, err := openWithRetry(dsn, 5, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect to mysql container:", err)
		return 1
	}
	defer db.Close()

	testDB = db
	return m.Run()
}

// openWithRetry exists because a container that just logged "ready" can still take a moment before
// it accepts TCP connections — a handful of short retries is cheap insurance against that race,
// versus an occasional flaky test failure that has nothing to do with the code being tested.
func openWithRetry(dsn string, attempts int, delay time.Duration) (*sql.DB, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		db, err := dbx.Open(dsn) // reuses the exact function production code uses to open+ping
		if err == nil {
			return db, nil
		}
		lastErr = err
		time.Sleep(delay)
	}
	return nil, lastErr
}

// migrationsDirPath resolves db/migrations relative to THIS file, using runtime.Caller rather than
// a relative path from the test binary's working directory — `go test` runs with the package
// directory as its working directory, but computing the path from the source file itself is more
// obviously correct to a reader than counting ".." segments against an assumption.
func migrationsDirPath() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "db", "migrations"), nil
}

// --- fixture helpers -------------------------------------------------------------------------
//
// Every test below inserts its own rows with a name unique to that test run (via uniqueSuffix) and
// never asserts on the TOTAL contents of a table — only on the rows it created itself. That's what
// lets every test share one container and one database without needing to reset state between them.

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func mustExec(t *testing.T, ctx context.Context, query string, args ...any) int {
	t.Helper()
	res, err := testDB.ExecContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("exec %q %v: %v", query, args, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id for %q: %v", query, err)
	}
	return int(id)
}

func insertCategory(t *testing.T, ctx context.Context, name string, isActive bool) int {
	t.Helper()
	return mustExec(t, ctx, `INSERT INTO category (name, is_active) VALUES (?, ?)`, name, isActive)
}

func insertProduct(t *testing.T, ctx context.Context, name, description string, isActive bool) int {
	t.Helper()
	return mustExec(t, ctx,
		`INSERT INTO product (name, description, is_active) VALUES (?, ?, ?)`,
		name, description, isActive,
	)
}

func linkProductCategory(t *testing.T, ctx context.Context, productID, categoryID int) {
	t.Helper()
	mustExec(t, ctx,
		`INSERT INTO product_category (product_id, category_id) VALUES (?, ?)`,
		productID, categoryID,
	)
}

func insertVariant(t *testing.T, ctx context.Context, productID int, sku, price string, stockQuantity int, isActive bool) int {
	t.Helper()
	return mustExec(t, ctx,
		`INSERT INTO product_variant (product_id, sku, price, stock_quantity, is_active) VALUES (?, ?, ?, ?, ?)`,
		productID, sku, price, stockQuantity, isActive,
	)
}

// insertAttribute attaches a name/value pair (e.g. "Color"/"Black") to a variant, upserting the
// shared attribute_name/attribute_value rows if a previous test already created that exact pair —
// attribute_name.name and attribute_value(attribute_name_id, value) both have UNIQUE constraints, so
// two tests both wanting "Color" must reuse the same row rather than collide.
func insertAttribute(t *testing.T, ctx context.Context, variantID int, name, value string) {
	t.Helper()

	mustExec(t, ctx, `INSERT INTO attribute_name (name) VALUES (?) ON DUPLICATE KEY UPDATE name = VALUES(name)`, name)
	var attributeNameID int
	if err := testDB.QueryRowContext(ctx, `SELECT attribute_name_id FROM attribute_name WHERE name = ?`, name).Scan(&attributeNameID); err != nil {
		t.Fatalf("look up attribute_name %q: %v", name, err)
	}

	mustExec(t, ctx,
		`INSERT INTO attribute_value (attribute_name_id, value) VALUES (?, ?) ON DUPLICATE KEY UPDATE value = VALUES(value)`,
		attributeNameID, value,
	)
	var attributeValueID int
	if err := testDB.QueryRowContext(ctx,
		`SELECT attribute_value_id FROM attribute_value WHERE attribute_name_id = ? AND value = ?`,
		attributeNameID, value,
	).Scan(&attributeValueID); err != nil {
		t.Fatalf("look up attribute_value %q=%q: %v", name, value, err)
	}

	mustExec(t, ctx,
		`INSERT INTO variant_attribute (variant_id, attribute_value_id) VALUES (?, ?)`,
		variantID, attributeValueID,
	)
}

// --- tests -------------------------------------------------------------------------------------

func TestCategoryRepository_List_ExcludesInactive(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	activeName := "Active Category " + suffix
	inactiveName := "Inactive Category " + suffix

	insertCategory(t, ctx, activeName, true)
	insertCategory(t, ctx, inactiveName, false)

	categories, err := NewCategoryRepository(testDB).List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	var sawActive, sawInactive bool
	for _, c := range categories {
		switch c.Name {
		case activeName:
			sawActive = true
		case inactiveName:
			sawInactive = true
		}
	}
	if !sawActive {
		t.Errorf("active category %q missing from List result", activeName)
	}
	if sawInactive {
		t.Errorf("inactive category %q should have been excluded (plan.md §6)", inactiveName)
	}
}

func TestProductRepository_GetByID_ReturnsFullDetailWithDerivedStockStatus(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()

	categoryID := insertCategory(t, ctx, "Speakers "+suffix, true)
	productID := insertProduct(t, ctx, "Bluetooth Speaker "+suffix, "Portable speaker", true)
	linkProductCategory(t, ctx, productID, categoryID)

	inStockVariantID := insertVariant(t, ctx, productID, "SPK-BLACK-"+suffix, "49.99", 3, true)
	insertAttribute(t, ctx, inStockVariantID, "Color", "Black")

	outOfStockVariantID := insertVariant(t, ctx, productID, "SPK-WHITE-"+suffix, "49.99", 0, true)
	insertAttribute(t, ctx, outOfStockVariantID, "Color", "White")

	repo := NewProductRepository(testDB)
	product, err := repo.GetByID(ctx, productID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if len(product.Categories) != 1 || product.Categories[0].ID != categoryID {
		t.Errorf("Categories = %+v, want exactly the one category just linked", product.Categories)
	}

	if len(product.Variants) != 2 {
		t.Fatalf("got %d variants, want 2", len(product.Variants))
	}

	byID := map[int]struct {
		wantStock domain.StockStatus
		wantColor string
	}{
		inStockVariantID:    {wantStock: domain.InStock, wantColor: "Black"},
		outOfStockVariantID: {wantStock: domain.OutOfStock, wantColor: "White"},
	}
	for _, v := range product.Variants {
		want, ok := byID[v.ID]
		if !ok {
			t.Fatalf("unexpected variant ID %d in result", v.ID)
		}
		if v.Price.String() != "49.99" {
			t.Errorf("variant %d price = %s, want 49.99", v.ID, v.Price.String())
		}
		if v.Stock != want.wantStock {
			t.Errorf("variant %d stock = %s, want %s", v.ID, v.Stock, want.wantStock)
		}
		if v.Attributes["Color"] != want.wantColor {
			t.Errorf("variant %d Color attribute = %q, want %q", v.ID, v.Attributes["Color"], want.wantColor)
		}
	}
}

// TestProductRepository_GetByID_UpdatedAtReflectsMostRecentVariantChange proves the actual point of
// product_variant's new updated_at column (migration 0003): a change to a VARIANT (price, stock —
// nothing about the product row itself) must still move product.UpdatedAt forward, since that's
// what httpapi's GetProduct derives its ETag from. Without this, a stock_quantity update would leave
// the ETag unchanged, and a client could keep getting a stale cached "IN_STOCK" from its own browser
// cache well past Cache-Control's 60-second window.
func TestProductRepository_GetByID_UpdatedAtReflectsMostRecentVariantChange(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()

	productID := insertProduct(t, ctx, "Timestamp Product "+suffix, "x", true)
	variantID := insertVariant(t, ctx, productID, "TS-"+suffix, "10.00", 1, true)

	repo := NewProductRepository(testDB)
	before, err := repo.GetByID(ctx, productID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if before.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt is zero, want a real timestamp from the product row")
	}

	// Force the variant's updated_at to a deterministic point far in the future — simulating "this
	// variant changed after the product itself was last touched" without a real sleep (MySQL's
	// DATETIME here only has 1-second resolution, so a short sleep would be both slow AND flaky).
	// An explicit SET value in an UPDATE is honored as-is; MySQL's "ON UPDATE CURRENT_TIMESTAMP" only
	// kicks in when the column is left for it to fill in automatically.
	future := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	mustExec(t, ctx, `UPDATE product_variant SET updated_at = ? WHERE variant_id = ?`, future, variantID)

	after, err := repo.GetByID(ctx, productID)
	if err != nil {
		t.Fatalf("GetByID (after variant update): %v", err)
	}
	if !after.UpdatedAt.Equal(future) {
		t.Errorf("UpdatedAt = %v, want %v (the variant's forced timestamp, now the most recent)", after.UpdatedAt, future)
	}
}

// TestProductRepository_GetByID_ExcludesDeactivatedVariant closes a real gap: every other variant
// fixture in this file passes isActive: true. FR-CATALOG-8 ("a deactivated product, VARIANT, or
// category must not appear") was only actually exercised here for products and categories — the
// `AND is_active = TRUE` filter in selectVariantsForProduct's SQL had no test proving it works.
func TestProductRepository_GetByID_ExcludesDeactivatedVariant(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()

	productID := insertProduct(t, ctx, "Multi Variant Product "+suffix, "x", true)
	activeVariantID := insertVariant(t, ctx, productID, "ACTIVE-"+suffix, "19.99", 5, true)
	deactivatedVariantID := insertVariant(t, ctx, productID, "DEACTIVATED-"+suffix, "19.99", 5, false)

	repo := NewProductRepository(testDB)
	product, err := repo.GetByID(ctx, productID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if len(product.Variants) != 1 {
		t.Fatalf("got %d variants, want 1 (the deactivated one should be excluded)", len(product.Variants))
	}
	if product.Variants[0].ID != activeVariantID {
		t.Errorf("remaining variant ID = %d, want the active one (%d)", product.Variants[0].ID, activeVariantID)
	}
	for _, v := range product.Variants {
		if v.ID == deactivatedVariantID {
			t.Errorf("deactivated variant %d appeared in GetByID result (FR-CATALOG-8)", deactivatedVariantID)
		}
	}
}

// TestProductRepository_List_ExcludesDeactivatedVariantFromBatch is the same check as above, but for
// List's SEPARATE query path (attachVariants' batched `product_id IN (...)` query) — a distinct piece
// of SQL from selectVariantsForProduct, so passing the GetByID test above doesn't prove this one
// also filters deactivated variants correctly.
func TestProductRepository_List_ExcludesDeactivatedVariantFromBatch(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	categoryID := insertCategory(t, ctx, "Variant Exclusion Category "+suffix, true)

	productID := insertProduct(t, ctx, "List Variant Exclusion Product "+suffix, "x", true)
	linkProductCategory(t, ctx, productID, categoryID)
	activeVariantID := insertVariant(t, ctx, productID, "LIST-ACTIVE-"+suffix, "9.99", 2, true)
	deactivatedVariantID := insertVariant(t, ctx, productID, "LIST-DEACTIVATED-"+suffix, "9.99", 2, false)

	repo := NewProductRepository(testDB)
	products, _, err := repo.List(ctx, app.ListFilter{CategoryID: &categoryID, Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("got %d products, want 1", len(products))
	}
	if len(products[0].Variants) != 1 || products[0].Variants[0].ID != activeVariantID {
		t.Errorf("product.Variants = %+v, want exactly the active variant (%d), not the deactivated one (%d)",
			products[0].Variants, activeVariantID, deactivatedVariantID)
	}
}

func TestProductRepository_GetByID_DeactivatedOrMissingBothReturnErrNotFound(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	repo := NewProductRepository(testDB)

	deactivatedID := insertProduct(t, ctx, "Deactivated Product "+suffix, "x", false)

	if _, err := repo.GetByID(ctx, deactivatedID); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("deactivated product: err = %v, want ErrNotFound (plan.md §6: gone means gone)", err)
	}

	const missingID = 987654321
	if _, err := repo.GetByID(ctx, missingID); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("nonexistent product: err = %v, want ErrNotFound", err)
	}
}

func TestProductRepository_List_SearchMatchesNameAndDescriptionCaseInsensitively(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	keyword := "Zylophone" + suffix // an invented, guaranteed-unique word

	matchByName := insertProduct(t, ctx, keyword+" Deluxe", "a musical instrument", true)
	matchByDescription := insertProduct(t, ctx, "Practice Instrument "+suffix, "the best "+keyword+" money can buy", true)
	noMatch := insertProduct(t, ctx, "Unrelated Product "+suffix, "nothing to do with it", true)
	insertVariant(t, ctx, matchByName, "SKU-A-"+suffix, "10.00", 1, true)
	insertVariant(t, ctx, matchByDescription, "SKU-B-"+suffix, "10.00", 1, true)
	insertVariant(t, ctx, noMatch, "SKU-C-"+suffix, "10.00", 1, true)

	repo := NewProductRepository(testDB)
	// lower-cased on purpose: FULLTEXT's default NATURAL LANGUAGE MODE is case-insensitive, and the
	// mixed-case product names above should still match a lowercase query (AC-CATALOG-2).
	products, _, err := repo.List(ctx, app.ListFilter{Query: lower(keyword), Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := map[int]bool{}
	for _, p := range products {
		found[p.ID] = true
	}
	if !found[matchByName] {
		t.Errorf("product matching by name (id %d) missing from search results", matchByName)
	}
	if !found[matchByDescription] {
		t.Errorf("product matching by description (id %d) missing from search results", matchByDescription)
	}
	if found[noMatch] {
		t.Errorf("unrelated product (id %d) should not match the search", noMatch)
	}
}

func TestProductRepository_List_FiltersByCategory(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()

	categoryID := insertCategory(t, ctx, "Filter Target Category "+suffix, true)
	inCategory := insertProduct(t, ctx, "In Category Product "+suffix, "x", true)
	notInCategory := insertProduct(t, ctx, "Other Category Product "+suffix, "x", true)
	linkProductCategory(t, ctx, inCategory, categoryID)
	insertVariant(t, ctx, inCategory, "SKU-CAT-A-"+suffix, "10.00", 1, true)
	insertVariant(t, ctx, notInCategory, "SKU-CAT-B-"+suffix, "10.00", 1, true)

	repo := NewProductRepository(testDB)
	products, _, err := repo.List(ctx, app.ListFilter{CategoryID: &categoryID, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := map[int]bool{}
	for _, p := range products {
		found[p.ID] = true
	}
	if !found[inCategory] {
		t.Errorf("product in the filtered category (id %d) missing from results", inCategory)
	}
	if found[notInCategory] {
		t.Errorf("product NOT in the filtered category (id %d) should not appear", notInCategory)
	}
}

func TestProductRepository_List_PaginationBoundaries(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	categoryID := insertCategory(t, ctx, "Pagination Category "+suffix, true)

	const productCount = 5
	for i := 0; i < productCount; i++ {
		id := insertProduct(t, ctx, fmt.Sprintf("Paged Product %s %d", suffix, i), "x", true)
		linkProductCategory(t, ctx, id, categoryID)
		insertVariant(t, ctx, id, fmt.Sprintf("SKU-PAGE-%s-%d", suffix, i), "10.00", 1, true)
	}

	repo := NewProductRepository(testDB)

	// Scope every page to this test's own category, so productCount and total line up exactly —
	// otherwise `total` would include every product every OTHER test in this file has inserted too.
	page1, total, err := repo.List(ctx, app.ListFilter{CategoryID: &categoryID, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if total != productCount {
		t.Fatalf("total = %d, want %d", total, productCount)
	}
	if len(page1) != 2 {
		t.Fatalf("page 1 length = %d, want 2", len(page1))
	}

	page3, _, err := repo.List(ctx, app.ListFilter{CategoryID: &categoryID, Page: 3, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(page3) != 1 {
		t.Fatalf("page 3 (the last, partial page) length = %d, want 1 (5 items, page size 2)", len(page3))
	}

	pageBeyondLast, _, err := repo.List(ctx, app.ListFilter{CategoryID: &categoryID, Page: 4, PageSize: 2})
	if err != nil {
		t.Fatalf("List page 4: %v", err)
	}
	if len(pageBeyondLast) != 0 {
		t.Errorf("page past the last one: got %d items, want 0, not an error (plan.md §6)", len(pageBeyondLast))
	}
}

func TestProductRepository_List_DeactivatedProductExcluded(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	categoryID := insertCategory(t, ctx, "Deactivated Test Category "+suffix, true)
	deactivatedID := insertProduct(t, ctx, "Deactivated List Product "+suffix, "x", false)
	linkProductCategory(t, ctx, deactivatedID, categoryID)
	insertVariant(t, ctx, deactivatedID, "SKU-DEACTIVATED-"+suffix, "10.00", 1, true)

	repo := NewProductRepository(testDB)
	products, total, err := repo.List(ctx, app.ListFilter{CategoryID: &categoryID, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0 (the only product in this category is deactivated)", total)
	}
	for _, p := range products {
		if p.ID == deactivatedID {
			t.Errorf("deactivated product %d appeared in List results (AC-CATALOG-6)", deactivatedID)
		}
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
