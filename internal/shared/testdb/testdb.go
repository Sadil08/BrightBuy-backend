//go:build integration

// Package testdb starts a throwaway MySQL in Docker (testcontainers), applies every real migration in
// db/migrations with golang-migrate — the same tool dev/prod use — and hands back a connection plus
// a few fixture helpers. It exists so feature integration tests are self-contained: no pre-seeded
// dev database, nothing skipped when an environment variable is missing (05_TESTING_STRATEGY.md).
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"brightbuy-backend/internal/shared/dbx"
)

// Start launches MySQL, migrates it to the latest version, and returns an open connection and a
// cleanup func that closes it and terminates the container. Call it from TestMain.
func Start(ctx context.Context, dbName string) (*sql.DB, func(), error) {
	container, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase(dbName),
		tcmysql.WithUsername("brightbuy_test"),
		tcmysql.WithPassword("test-password"),
		// stored functions/procedures need this when binary logging is on (the image default)
		testcontainers.WithCmd("--log-bin-trust-function-creators=1", "--default-time-zone=+00:00"),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start mysql container: %w", err)
	}
	cleanup := func() { _ = testcontainers.TerminateContainer(container) }

	dsn, err := container.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("connection string: %w", err)
	}

	_, thisFile, _, _ := runtime.Caller(0)
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "db", "migrations")
	migrator, err := migrate.New((&url.URL{Scheme: "file", Opaque: filepath.ToSlash(dir)}).String(), "mysql://"+dsn)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("create migrator: %w", err)
	}
	if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
		cleanup()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	var db *sql.DB
	for i := 0; i < 5; i++ {
		if db, err = dbx.Open(dsn); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	return db, func() { _ = db.Close(); cleanup() }, nil
}

// Exec runs a statement and returns its LastInsertId, failing the test on any error.
func Exec(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("exec %q %v: %v", query, args, err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// Int runs a single-value query and returns it as an int.
func Int(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var v int
	if err := db.QueryRow(query, args...).Scan(&v); err != nil {
		t.Fatalf("query %q %v: %v", query, args, err)
	}
	return v
}

// Customer inserts a CUSTOMER account with its profile and returns (userAccountID, customerID).
func Customer(t *testing.T, db *sql.DB, email string) (int, int) {
	t.Helper()
	roleID := Int(t, db, `SELECT role_id FROM role WHERE name = 'CUSTOMER'`)
	userID := Exec(t, db, `INSERT INTO user_account (email, password_hash, role_id) VALUES (?, 'hash', ?)`, email, roleID)
	return userID, Exec(t, db, `INSERT INTO customer (user_account_id, name, phone) VALUES (?, 'Test', '0770000000')`, userID)
}

// StaffUser inserts an account with the given role name and returns its user_account_id.
func StaffUser(t *testing.T, db *sql.DB, email, role string) int {
	t.Helper()
	roleID := Int(t, db, `SELECT role_id FROM role WHERE name = ?`, role)
	return Exec(t, db, `INSERT INTO user_account (email, password_hash, role_id) VALUES (?, 'hash', ?)`, email, roleID)
}

// Variant inserts an active product with one active variant and returns the variant id.
func Variant(t *testing.T, db *sql.DB, productName, sku, price string, stock int) int {
	t.Helper()
	pid := Exec(t, db, `INSERT INTO product (name) VALUES (?)`, productName)
	return Exec(t, db, `INSERT INTO product_variant (product_id, sku, price, stock_quantity) VALUES (?, ?, ?, ?)`, pid, sku, price, stock)
}

// City inserts a city with the given classification ('Main' or 'Other') and returns its id.
func City(t *testing.T, db *sql.DB, name, classification string) int {
	t.Helper()
	return Exec(t, db, `INSERT INTO city (name, classification) VALUES (?, ?)`, name, classification)
}

// PlaceOrder calls sp_place_order (the same procedure checkout calls) for one customer and returns
// the new order id. cityID 0 means NULL (Store Pickup).
func PlaceOrder(t *testing.T, db *sql.DB, customerID int, itemsJSON, mode string, cityID int, idemKey string) int {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Conn(ctx) // OUT parameters are session variables: stay on one connection
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var city any
	var addr any
	if cityID != 0 {
		city, addr = cityID, "1 Test Street"
	}
	if _, err := conn.ExecContext(ctx,
		`CALL sp_place_order(?, ?, ?, ?, ?, 'COD', ?, 0, 0, NULL, NULL, @oid, @created)`,
		customerID, itemsJSON, mode, city, addr, idemKey); err != nil {
		t.Fatalf("sp_place_order: %v", err)
	}
	var orderID int
	if err := conn.QueryRowContext(ctx, `SELECT @oid`).Scan(&orderID); err != nil {
		t.Fatal(err)
	}
	return orderID
}
