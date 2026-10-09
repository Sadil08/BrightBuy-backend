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

	"brightbuy-backend/internal/ordering/app"
	orderdomain "brightbuy-backend/internal/ordering/domain"
	"brightbuy-backend/internal/shared/dbx"

	mysqlDriver "github.com/go-sql-driver/mysql"
	migrate "github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	mysqlcontainer "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	checkoutTestOnce      sync.Once
	checkoutTestDB        *sql.DB
	checkoutTestContainer *mysqlcontainer.MySQLContainer
	checkoutTestErr       error
)

func TestCheckoutMigrationsAndOrderRepository(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	db := checkoutIntegrationDB(t, ctx)
	firstCustomerID := createIntegrationCustomer(t, db, "checkout-one@example.test")
	secondCustomerID := createIntegrationCustomer(t, db, "checkout-two@example.test")
	mainCityID := createIntegrationCity(t, db, "Integration Main", "Main")
	otherCityID := createIntegrationCity(t, db, "Integration Other", "Other")
	availableVariantID := createIntegrationVariant(t, db, "checkout-available", 20, "12.50")
	oneLeftVariantID := createIntegrationVariant(t, db, "checkout-one-left", 1, "3.00")
	outOfStockVariantID := createIntegrationVariant(t, db, "checkout-out-of-stock", 0, "1.00")

	repository := NewOrderRepository(db)
	cityType, err := repository.CityExists(ctx, mainCityID)
	if err != nil || !cityType {
		t.Fatalf("CityExists(%d) = %v, %v", mainCityID, cityType, err)
	}

	// The stored function differentiates delivery regions and adds three days only
	// when an ordered variant has no remaining stock.
	for _, test := range []struct {
		name     string
		mode     string
		cityID   int
		variant  int
		quantity int
		wantDays int
	}{
		{"main-city", string(orderdomain.StandardDelivery), mainCityID, availableVariantID, 1, 5},
		{"other-city", string(orderdomain.StandardDelivery), otherCityID, availableVariantID, 1, 7},
		{"store-pickup", string(orderdomain.StorePickup), 0, availableVariantID, 1, 0},
		{"main-city-out-of-stock", string(orderdomain.StandardDelivery), mainCityID, outOfStockVariantID, 1, 8},
		{"store-pickup-out-of-stock", string(orderdomain.StorePickup), 0, outOfStockVariantID, 1, 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			var city any
			if test.cityID != 0 {
				city = test.cityID
			}
			var days int
			err := db.QueryRowContext(ctx, `
				SELECT fn_estimate_delivery_days(?, ?, JSON_ARRAY(JSON_OBJECT('variant_id', ?, 'quantity', ?)))
			`, test.mode, city, test.variant, test.quantity).Scan(&days)
			if err != nil {
				t.Fatalf("estimate delivery days: %v", err)
			}
			if days != test.wantDays {
				t.Fatalf("estimated days = %d, want %d", days, test.wantDays)
			}
		})
	}

	command := app.PlaceOrderCommand{
		CustomerID:   firstCustomerID,
		ItemsJSON:    []byte(fmt.Sprintf(`[{"variant_id":%d,"quantity":2}]`, availableVariantID)),
		DeliveryMode: orderdomain.StandardDelivery, DeliveryCityID: &mainCityID,
		DeliveryAddress: "1 Integration Way", PaymentMethod: orderdomain.PaymentCOD,
		IdempotencyKey: "integration-idempotent", TaxAmountCents: 206, DeliveryFeeCents: 999,
	}
	var orderID int
	var created bool
	err = dbx.WithTx(ctx, db, func(tx *sql.Tx) error {
		var callErr error
		orderID, created, callErr = repository.CallPlaceOrder(ctx, tx, command)
		return callErr
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if !created || orderID <= 0 {
		t.Fatalf("first checkout returned order=%d created=%v", orderID, created)
	}

	// A replay must return the prior order and must not decrement stock again.
	var replayID int
	var replayCreated bool
	err = dbx.WithTx(ctx, db, func(tx *sql.Tx) error {
		var callErr error
		replayID, replayCreated, callErr = repository.CallPlaceOrder(ctx, tx, command)
		return callErr
	})
	if err != nil {
		t.Fatalf("replay order: %v", err)
	}
	if replayID != orderID || replayCreated {
		t.Fatalf("replay returned order=%d created=%v, want order=%d created=false", replayID, replayCreated, orderID)
	}
	assertIntQuery(t, db, `SELECT stock_quantity FROM product_variant WHERE variant_id = ?`, availableVariantID, 18)
	assertIntQuery(t, db, `SELECT COUNT(*) FROM stock_movement WHERE related_order_id = ?`, orderID, 1)
	assertIntQuery(t, db, `SELECT change_qty FROM stock_movement WHERE related_order_id = ?`, orderID, -2)
	assertIntQuery(t, db, `SELECT COUNT(*) FROM order_status_history WHERE order_id = ?`, orderID, 1)
	assertStringQuery(t, db, `SELECT status FROM order_status_history WHERE order_id = ?`, orderID, "Confirmed")
	assertStringQuery(t, db, `SELECT unit_price_at_order FROM order_item WHERE order_id = ?`, orderID, "12.50")
	if _, err := db.ExecContext(ctx, `UPDATE product_variant SET price = '99.99' WHERE variant_id = ?`, availableVariantID); err != nil {
		t.Fatalf("change catalog price after order: %v", err)
	}
	placedOrder, err := repository.GetByID(ctx, firstCustomerID, orderID)
	if err != nil {
		t.Fatalf("GetByID for owner: %v", err)
	}
	if len(placedOrder.Items) != 1 || placedOrder.Items[0].UnitPriceAtOrder != "12.50" {
		t.Fatalf("order price snapshot changed: %+v", placedOrder.Items)
	}
	listedOrders, total, err := repository.ListByCustomer(ctx, firstCustomerID, 1, 20)
	if err != nil || total != 1 || len(listedOrders) != 1 || listedOrders[0].ID != orderID {
		t.Fatalf("ListByCustomer returned orders=%+v total=%d err=%v", listedOrders, total, err)
	}

	if _, err := repository.GetByID(ctx, secondCustomerID, orderID); !errors.Is(err, app.ErrOrderNotFound) {
		t.Fatalf("GetByID for different customer error = %v, want ErrOrderNotFound", err)
	}

	// An insufficient-stock failure rolls the whole transaction back.
	stockCommand := app.PlaceOrderCommand{
		CustomerID: firstCustomerID,
		ItemsJSON: []byte(fmt.Sprintf(
			`[{"variant_id":%d,"quantity":1},{"variant_id":%d,"quantity":2}]`,
			availableVariantID, oneLeftVariantID,
		)),
		DeliveryMode: orderdomain.StorePickup, PaymentMethod: orderdomain.PaymentCOD,
		IdempotencyKey: "integration-stock-failure",
	}
	err = dbx.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, _, callErr := repository.CallPlaceOrder(ctx, tx, stockCommand)
		return callErr
	})
	if !errors.Is(err, app.ErrStockExceeded) {
		t.Fatalf("insufficient-stock error = %v, want ErrStockExceeded", err)
	}
	assertIntQuery(t, db, `SELECT stock_quantity FROM product_variant WHERE variant_id = ?`, availableVariantID, 18)
	assertIntQuery(t, db, `SELECT stock_quantity FROM product_variant WHERE variant_id = ?`, oneLeftVariantID, 1)
	assertIntQuery(t, db, "SELECT COUNT(*) FROM `order` WHERE customer_id = ? AND idempotency_key = ?", firstCustomerID, "integration-stock-failure", 0)

	// Two customers competing for the final unit cannot both create an order.
	type result struct {
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, customerID := range []int{firstCustomerID, secondCustomerID} {
		customerID := customerID
		go func() {
			<-start
			concurrentCommand := app.PlaceOrderCommand{
				CustomerID:   customerID,
				ItemsJSON:    []byte(fmt.Sprintf(`[{"variant_id":%d,"quantity":1}]`, oneLeftVariantID)),
				DeliveryMode: orderdomain.StorePickup, PaymentMethod: orderdomain.PaymentCOD,
				IdempotencyKey: fmt.Sprintf("integration-concurrent-%d", customerID),
			}
			err := dbx.WithTx(ctx, db, func(tx *sql.Tx) error {
				_, _, callErr := repository.CallPlaceOrder(ctx, tx, concurrentCommand)
				return callErr
			})
			results <- result{err: err}
		}()
	}
	close(start)
	successes, stockFailures := 0, 0
	for range 2 {
		got := <-results
		switch {
		case got.err == nil:
			successes++
		case errors.Is(got.err, app.ErrStockExceeded):
			stockFailures++
		default:
			t.Fatalf("concurrent checkout failed unexpectedly: %v", got.err)
		}
	}
	if successes != 1 || stockFailures != 1 {
		t.Fatalf("concurrent outcomes: successes=%d stock failures=%d, want 1 each", successes, stockFailures)
	}
	assertIntQuery(t, db, `SELECT stock_quantity FROM product_variant WHERE variant_id = ?`, oneLeftVariantID, 0)
	assertIntQuery(t, db, `SELECT COUNT(*) FROM stock_movement WHERE variant_id = ?`, oneLeftVariantID, 1)

	// The database trigger also protects against stock changes outside checkout.
	_, err = db.ExecContext(ctx, `UPDATE product_variant SET stock_quantity = -1 WHERE variant_id = ?`, oneLeftVariantID)
	var mysqlErr *mysqlDriver.MySQLError
	if !errors.As(err, &mysqlErr) || mysqlErr.Message != "STOCK_CANNOT_BE_NEGATIVE" {
		t.Fatalf("negative-stock update error = %v, want trigger signal STOCK_CANNOT_BE_NEGATIVE", err)
	}
}

func checkoutIntegrationDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	checkoutTestOnce.Do(func() {
		checkoutTestContainer, checkoutTestErr = mysqlcontainer.Run(
			ctx,
			"mysql:8.0",
			mysqlcontainer.WithDatabase("brightbuy_checkout_test"),
			mysqlcontainer.WithUsername("root"),
			mysqlcontainer.WithPassword("brightbuy-test-password"),
			testcontainers.WithWaitStrategy(wait.ForLog("port: 3306  MySQL Community Server").WithStartupTimeout(90*time.Second)),
		)
		if checkoutTestErr != nil {
			return
		}

		connectionString, err := checkoutTestContainer.ConnectionString(ctx, "parseTime=true&multiStatements=true")
		if err != nil {
			checkoutTestErr = fmt.Errorf("get MySQL test connection string: %w", err)
			return
		}
		checkoutTestDB, checkoutTestErr = sql.Open("mysql", connectionString)
		if checkoutTestErr != nil {
			return
		}
		checkoutTestDB.SetMaxOpenConns(10)
		checkoutTestDB.SetMaxIdleConns(10)
		if err := checkoutTestDB.PingContext(ctx); err != nil {
			checkoutTestErr = fmt.Errorf("ping MySQL test database: %w", err)
			return
		}

		_, filename, _, ok := runtime.Caller(0)
		if !ok {
			checkoutTestErr = errors.New("locate integration test source")
			return
		}
		migrationDirectory, err := filepath.Abs(filepath.Join(filepath.Dir(filename), "..", "..", "..", "db", "migrations"))
		if err != nil {
			checkoutTestErr = fmt.Errorf("resolve migration directory: %w", err)
			return
		}
		if _, err := os.Stat(migrationDirectory); err != nil {
			checkoutTestErr = fmt.Errorf("find migrations at %q: %w", migrationDirectory, err)
			return
		}
		sourceURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(migrationDirectory)}).String()
		databaseURL := "mysql://" + connectionString
		migrator, err := migrate.New(sourceURL, databaseURL)
		if err != nil {
			checkoutTestErr = fmt.Errorf("create migration runner: %w", err)
			return
		}
		defer migrator.Close()
		if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			checkoutTestErr = fmt.Errorf("apply migrations to disposable MySQL: %w", err)
			return
		}
		if err := migrator.Down(); err != nil {
			checkoutTestErr = fmt.Errorf("roll back migrations on disposable MySQL: %w", err)
			return
		}
		if err := migrator.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			checkoutTestErr = fmt.Errorf("reapply migrations to disposable MySQL: %w", err)
			return
		}
	})
	if checkoutTestErr != nil {
		t.Fatalf("prepare disposable MySQL test database: %v", checkoutTestErr)
	}
	t.Cleanup(func() {
		if checkoutTestDB != nil {
			_ = checkoutTestDB.Close()
		}
		if checkoutTestContainer != nil {
			_ = checkoutTestContainer.Terminate(context.Background())
		}
	})
	return checkoutTestDB
}

func createIntegrationCustomer(t *testing.T, db *sql.DB, email string) int {
	t.Helper()
	var roleID int
	if err := db.QueryRow(`SELECT role_id FROM role WHERE name = 'CUSTOMER'`).Scan(&roleID); err != nil {
		t.Fatalf("find customer role: %v", err)
	}
	result, err := db.Exec(`INSERT INTO user_account (email, password_hash, role_id) VALUES (?, 'integration-hash', ?)`, email, roleID)
	if err != nil {
		t.Fatalf("insert integration account: %v", err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get integration account ID: %v", err)
	}
	result, err = db.Exec(`INSERT INTO customer (user_account_id, name, phone) VALUES (?, 'Integration Customer', '0770000000')`, userID)
	if err != nil {
		t.Fatalf("insert integration customer: %v", err)
	}
	customerID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get integration customer ID: %v", err)
	}
	return int(customerID)
}

func createIntegrationCity(t *testing.T, db *sql.DB, name, classification string) int {
	t.Helper()
	result, err := db.Exec(`INSERT INTO city (name, classification) VALUES (?, ?)`, name, classification)
	if err != nil {
		t.Fatalf("insert integration city: %v", err)
	}
	cityID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get integration city ID: %v", err)
	}
	return int(cityID)
}

func createIntegrationVariant(t *testing.T, db *sql.DB, sku string, stock int, price string) int {
	t.Helper()
	result, err := db.Exec(`INSERT INTO product (name) VALUES (?)`, "Integration "+sku)
	if err != nil {
		t.Fatalf("insert integration product: %v", err)
	}
	productID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get integration product ID: %v", err)
	}
	result, err = db.Exec(`INSERT INTO product_variant (product_id, sku, price, stock_quantity) VALUES (?, ?, ?, ?)`,
		productID, sku, price, stock)
	if err != nil {
		t.Fatalf("insert integration variant: %v", err)
	}
	variantID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("get integration variant ID: %v", err)
	}
	return int(variantID)
}

func assertIntQuery(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	want := args[len(args)-1].(int)
	queryArgs := args[:len(args)-1]
	var got int
	if err := db.QueryRow(query, queryArgs...).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q returned %d, want %d", query, got, want)
	}
}

func assertStringQuery(t *testing.T, db *sql.DB, query string, id int, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(query, id).Scan(&got); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("query %q returned %q, want %q", query, got, want)
	}
}
