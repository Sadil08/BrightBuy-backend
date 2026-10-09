//go:build integration

package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	cartapp "brightbuy-backend/internal/cart/app"
	"brightbuy-backend/internal/cart/domain"
	catalogapp "brightbuy-backend/internal/catalog/app"
	catalogmysql "brightbuy-backend/internal/catalog/mysql"
	"brightbuy-backend/internal/shared/dbx"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

func runIntegrationTests(m *testing.M) int {
	ctx := context.Background()
	container, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("brightbuy_cart_test"),
		tcmysql.WithUsername("brightbuy_test"),
		tcmysql.WithPassword("test-password"),
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

	dir, err := migrationsDirPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "locate migrations dir:", err)
		return 1
	}
	sourceURL := (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(dir)}).String()
	migrator, err := migrate.New(sourceURL, "mysql://"+dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create migrate instance:", err)
		return 1
	}
	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		fmt.Fprintln(os.Stderr, "run migrations:", err)
		return 1
	}

	testDB, err = openWithRetry(dsn, 5, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect to mysql container:", err)
		return 1
	}
	defer testDB.Close()

	return m.Run()
}

func openWithRetry(dsn string, attempts int, delay time.Duration) (*sql.DB, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		db, err := dbx.Open(dsn)
		if err == nil {
			return db, nil
		}
		lastErr = err
		time.Sleep(delay)
	}
	return nil, lastErr
}

func migrationsDirPath() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "db", "migrations"), nil
}

func uniqueSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func mustExec(t *testing.T, ctx context.Context, query string, args ...any) int {
	t.Helper()
	result, err := testDB.ExecContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("exec %q %v: %v", query, args, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id for %q: %v", query, err)
	}
	return int(id)
}

func insertCustomer(t *testing.T, ctx context.Context, suffix string) int {
	t.Helper()
	var roleID int
	if err := testDB.QueryRowContext(ctx, `SELECT role_id FROM role WHERE name = 'CUSTOMER'`).Scan(&roleID); err != nil {
		t.Fatalf("find CUSTOMER role: %v", err)
	}
	userID := mustExec(t, ctx,
		`INSERT INTO user_account (email, password_hash, role_id) VALUES (?, ?, ?)`,
		"cart-integration-"+suffix+"@example.test", "integration-test-hash", roleID,
	)
	return mustExec(t, ctx,
		`INSERT INTO customer (user_account_id, name, phone) VALUES (?, ?, ?)`,
		userID, "Cart Integration "+suffix, "+15550000000",
	)
}

func insertProduct(t *testing.T, ctx context.Context, name string, isActive bool) int {
	t.Helper()
	return mustExec(t, ctx,
		`INSERT INTO product (name, description, is_active) VALUES (?, ?, ?)`,
		name, "Cart integration fixture", isActive,
	)
}

func insertVariant(t *testing.T, ctx context.Context, productID int, sku, price string, stock int, isActive bool) int {
	t.Helper()
	return mustExec(t, ctx,
		`INSERT INTO product_variant (product_id, sku, price, stock_quantity, is_active) VALUES (?, ?, ?, ?, ?)`,
		productID, sku, price, stock, isActive,
	)
}

func newIntegrationService() (*cartapp.Service, *CartRepository) {
	cartRepo := NewCartRepository(testDB)
	products := catalogmysql.NewProductRepository(testDB)
	catalog := catalogapp.NewCatalogService(products, nil)
	return cartapp.NewService(cartRepo, catalog), cartRepo
}

func TestCartRepository_FindLineByIDIsOwnershipScoped(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	ownerID := insertCustomer(t, ctx, "owner-"+suffix)
	otherID := insertCustomer(t, ctx, "other-"+suffix)
	productID := insertProduct(t, ctx, "Ownership Product "+suffix, true)
	variantID := insertVariant(t, ctx, productID, "OWN-"+suffix, "10.00", 5, true)
	repo := NewCartRepository(testDB)

	cart, err := repo.UpsertLine(ctx, ownerID, variantID, 2)
	if err != nil {
		t.Fatalf("UpsertLine: %v", err)
	}
	if len(cart.Items) != 1 {
		t.Fatalf("owner cart has %d items, want 1", len(cart.Items))
	}
	lineID := cart.Items[0].ID

	got, err := repo.FindLineByID(ctx, otherID, lineID)
	if err != nil {
		t.Fatalf("FindLineByID as another customer: %v", err)
	}
	if got != nil {
		t.Fatalf("another customer found cart line %+v; want not found", got)
	}
	got, err = repo.FindLineByID(ctx, ownerID, lineID)
	if err != nil || got == nil || got.VariantID != variantID {
		t.Fatalf("owner FindLineByID = %+v, %v; want variant %d", got, err, variantID)
	}
}

func TestCartServiceGetCartUsesCurrentCatalogPrice(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	customerID := insertCustomer(t, ctx, suffix)
	productID := insertProduct(t, ctx, "Current Price Product "+suffix, true)
	variantID := insertVariant(t, ctx, productID, "PRICE-"+suffix, "9.99", 10, true)
	service, repo := newIntegrationService()
	if _, err := repo.UpsertLine(ctx, customerID, variantID, 2); err != nil {
		t.Fatalf("UpsertLine: %v", err)
	}

	first, err := service.GetCart(ctx, customerID)
	if err != nil {
		t.Fatalf("GetCart before price change: %v", err)
	}
	if len(first.Items) != 1 || first.Items[0].UnitPrice != domain.Money(999) {
		t.Fatalf("first cart = %+v, want unit price 999 cents", first)
	}

	if _, err := testDB.ExecContext(ctx, `UPDATE product_variant SET price = ? WHERE variant_id = ?`, "12.34", variantID); err != nil {
		t.Fatalf("update current price: %v", err)
	}
	second, err := service.GetCart(ctx, customerID)
	if err != nil {
		t.Fatalf("GetCart after price change: %v", err)
	}
	if second.Items[0].UnitPrice != domain.Money(1234) || second.Items[0].LineTotal != domain.Money(2468) || second.Subtotal != domain.Money(2468) {
		t.Errorf("cart after price change = %+v, want current 12.34 price and 24.68 total", second)
	}
}

func TestCartServiceGetCartExcludesDeactivatedItemsFromSubtotal(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	customerID := insertCustomer(t, ctx, suffix)
	activeProductID := insertProduct(t, ctx, "Active Product "+suffix, true)
	inactiveProductID := insertProduct(t, ctx, "Product To Deactivate "+suffix, true)
	activeVariantID := insertVariant(t, ctx, activeProductID, "ACTIVE-"+suffix, "2.50", 10, true)
	variantToDeactivateID := insertVariant(t, ctx, activeProductID, "VARIANT-OFF-"+suffix, "8.00", 10, true)
	productVariantID := insertVariant(t, ctx, inactiveProductID, "PRODUCT-OFF-"+suffix, "20.00", 10, true)
	service, repo := newIntegrationService()

	for _, variantID := range []int{activeVariantID, variantToDeactivateID, productVariantID} {
		if _, err := repo.UpsertLine(ctx, customerID, variantID, 2); err != nil {
			t.Fatalf("UpsertLine variant %d: %v", variantID, err)
		}
	}
	if _, err := testDB.ExecContext(ctx, `UPDATE product_variant SET is_active = FALSE WHERE variant_id = ?`, variantToDeactivateID); err != nil {
		t.Fatalf("deactivate variant: %v", err)
	}
	if _, err := testDB.ExecContext(ctx, `UPDATE product SET is_active = FALSE WHERE product_id = ?`, inactiveProductID); err != nil {
		t.Fatalf("deactivate product: %v", err)
	}

	cart, err := service.GetCart(ctx, customerID)
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if len(cart.Items) != 3 {
		t.Fatalf("got %d cart items, want 3 including unavailable lines", len(cart.Items))
	}
	items := make(map[int]domain.CartItem, len(cart.Items))
	for _, item := range cart.Items {
		items[item.VariantID] = item
	}
	if items[activeVariantID].Unavailable {
		t.Errorf("active variant %d marked unavailable", activeVariantID)
	}
	for _, variantID := range []int{variantToDeactivateID, productVariantID} {
		if !items[variantID].Unavailable {
			t.Errorf("deactivated variant %d not marked unavailable", variantID)
		}
	}
	if cart.Subtotal != domain.Money(500) {
		t.Errorf("Subtotal = %d cents, want 500 cents from the active line only", cart.Subtotal)
	}
}

func TestCartRepository_ConcurrentUpsertsKeepOneLine(t *testing.T) {
	ctx := context.Background()
	suffix := uniqueSuffix()
	customerID := insertCustomer(t, ctx, suffix)
	productID := insertProduct(t, ctx, "Concurrent Product "+suffix, true)
	variantID := insertVariant(t, ctx, productID, "CONCURRENT-"+suffix, "1.00", 50, true)
	repo := NewCartRepository(testDB)

	const writers = 12
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 1; i <= writers; i++ {
		wg.Add(1)
		go func(quantity int) {
			defer wg.Done()
			<-start
			_, err := repo.UpsertLine(ctx, customerID, variantID, quantity)
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent UpsertLine: %v", err)
		}
	}

	cart, err := repo.Get(ctx, customerID)
	if err != nil {
		t.Fatalf("Get after concurrent upserts: %v", err)
	}
	if len(cart.Items) != 1 {
		t.Fatalf("got %d cart lines, want exactly 1", len(cart.Items))
	}
	if cart.Items[0].VariantID != variantID || cart.Items[0].Quantity < 1 || cart.Items[0].Quantity > writers {
		t.Errorf("cart line = %+v, want the shared variant and one writer's quantity", cart.Items[0])
	}
}
