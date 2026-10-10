package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"brightbuy-backend/internal/shared/dbx"
)

func TestOrderStatusSQL_TransitionAndHistory(t *testing.T) {
	db := orderStatusTestDB(t)
	ctx := context.Background()
	assertOrderStatusProceduresInstalled(t, db)

	orderID := createOrderStatusFixture(t, db, "Confirmed", 0)
	t.Cleanup(func() { cleanupOrderStatusFixture(t, db, orderID) })

	if _, err := db.ExecContext(ctx, "SET @acting_user_id = NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE `order` SET status = 'Processing' WHERE order_id = ?", orderID); err != nil {
		t.Fatalf("valid transition: %v", err)
	}

	var historyCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_status_history WHERE order_id = ? AND status = 'Processing'`, orderID).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if historyCount != 1 {
		t.Fatalf("history rows = %d, want 1", historyCount)
	}

	if _, err := db.ExecContext(ctx, "UPDATE `order` SET status = 'Shipped' WHERE order_id = ?", orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE `order` SET status = 'Delivered' WHERE order_id = ?", orderID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE `order` SET status = 'Processing' WHERE order_id = ?", orderID); err == nil {
		t.Fatal("expected Delivered -> Processing to be rejected by SQL trigger")
	}

	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM `order` WHERE order_id = ?", orderID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "Delivered" {
		t.Fatalf("status after rejected transition = %s, want Delivered", status)
	}
}

func TestOrderStatusSQL_CancelReturnsStockAndRejectsDelivered(t *testing.T) {
	db := orderStatusTestDB(t)
	ctx := context.Background()
	assertOrderStatusProceduresInstalled(t, db)

	variantID := createOrderStatusVariant(t, db, 5)
	orderID := createOrderStatusFixture(t, db, "Confirmed", variantID)
	t.Cleanup(func() { cleanupOrderStatusFixture(t, db, orderID) })

	if _, err := db.ExecContext(ctx, "CALL sp_cancel_order(?, ?)", orderID, 0); err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	var status string
	var stock int
	if err := db.QueryRowContext(ctx, "SELECT status FROM `order` WHERE order_id = ?", orderID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT stock_quantity FROM product_variant WHERE variant_id = ?", variantID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if status != "Cancelled" || stock != 7 {
		t.Fatalf("cancel result status=%s stock=%d, want Cancelled and 7", status, stock)
	}

	var movementCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stock_movement WHERE related_order_id = ? AND change_qty = 2`, orderID).Scan(&movementCount); err != nil {
		t.Fatal(err)
	}
	if movementCount != 1 {
		t.Fatalf("cancellation movements = %d, want 1", movementCount)
	}

	deliveredOrderID := createOrderStatusFixture(t, db, "Delivered", variantID)
	t.Cleanup(func() { cleanupOrderStatusFixture(t, db, deliveredOrderID) })
	if _, err := db.ExecContext(ctx, "CALL sp_cancel_order(?, ?)", deliveredOrderID, 0); err == nil {
		t.Fatal("expected cancellation of Delivered order to be rejected")
	}
	if err := db.QueryRowContext(ctx, "SELECT stock_quantity FROM product_variant WHERE variant_id = ?", variantID).Scan(&stock); err != nil {
		t.Fatal(err)
	}
	if stock != 7 {
		t.Fatalf("stock after rejected delivered cancellation = %d, want 7", stock)
	}
}

func orderStatusTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("BRIGHTBUY_TEST_DSN")
	if dsn == "" {
		dsn = "brightbuy_app:devapppass@tcp(127.0.0.1:3307)/brightbuy?parseTime=true"
	}
	db, err := dbx.Open(dsn)
	if err != nil {
		t.Skipf("integration database unavailable: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func assertOrderStatusProceduresInstalled(t *testing.T, db *sql.DB) {
	t.Helper()
	var triggerCount, procedureCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.triggers WHERE trigger_schema = DATABASE() AND trigger_name = 'trg_order_status_transition'`).Scan(&triggerCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.routines WHERE routine_schema = DATABASE() AND routine_name = 'sp_cancel_order'`).Scan(&procedureCount); err != nil {
		t.Fatal(err)
	}
	if triggerCount == 0 || procedureCount == 0 {
		t.Skip("apply migration 0012_order_status_procedures before running order-status SQL integration tests")
	}
}

func createOrderStatusFixture(t *testing.T, db *sql.DB, status string, variantID int64) int64 {
	t.Helper()
	var customerID int64
	if err := db.QueryRow("SELECT customer_id FROM customer ORDER BY customer_id LIMIT 1").Scan(&customerID); err != nil {
		var roleID int64
		if roleErr := db.QueryRow("SELECT role_id FROM role WHERE name = 'CUSTOMER'").Scan(&roleID); roleErr != nil {
			t.Skipf("checkout fixture unavailable: %v", err)
		}
		result, userErr := db.Exec("INSERT INTO user_account (email, password_hash, role_id) VALUES (?, ?, ?)", uniqueOrderStatusValue()+"@test.local", "integration-test", roleID)
		if userErr != nil {
			t.Fatalf("create customer user fixture: %v", userErr)
		}
		userID, userErr := result.LastInsertId()
		if userErr != nil {
			t.Fatal(userErr)
		}
		result, userErr = db.Exec("INSERT INTO customer (user_account_id, name, phone) VALUES (?, 'Order Status Test', '0000000000')", userID)
		if userErr != nil {
			t.Fatalf("create customer fixture: %v", userErr)
		}
		customerID, userErr = result.LastInsertId()
		if userErr != nil {
			t.Fatal(userErr)
		}
		t.Cleanup(func() { _, _ = db.Exec("DELETE FROM customer WHERE customer_id = ?", customerID) })
	}
	result, err := db.Exec("INSERT INTO `order` (customer_id, status, subtotal, tax_amount, delivery_fee, total_amount, idempotency_key) VALUES (?, ?, 1.00, 0.00, 0.00, 1.00, ?)", customerID, status, uniqueOrderStatusValue())
	if err != nil {
		t.Fatalf("create order fixture: %v", err)
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if variantID != 0 {
		if _, err := db.Exec(`INSERT INTO order_item (order_id, variant_id, quantity, unit_price_at_order) VALUES (?, ?, 2, 1.00)`, orderID, variantID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO order_status_history (order_id, status, changed_by_user_id) VALUES (?, ?, NULL)`, orderID, status); err != nil {
		t.Fatal(err)
	}
	return orderID
}

func createOrderStatusVariant(t *testing.T, db *sql.DB, stock int) int64 {
	t.Helper()
	result, err := db.Exec(`INSERT INTO product (name, description, is_active) VALUES (?, '', TRUE)`, uniqueOrderStatusValue())
	if err != nil {
		t.Fatal(err)
	}
	productID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM product WHERE product_id = ?`, productID) })
	result, err = db.Exec(`INSERT INTO product_variant (product_id, sku, price, stock_quantity, is_active) VALUES (?, ?, 1.00, ?, TRUE)`, productID, uniqueOrderStatusValue(), stock)
	if err != nil {
		t.Fatal(err)
	}
	variantID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return variantID
}

func cleanupOrderStatusFixture(t *testing.T, db *sql.DB, orderID int64) {
	t.Helper()
	_, _ = db.Exec(`DELETE FROM stock_movement WHERE related_order_id = ?`, orderID)
	_, _ = db.Exec(`DELETE FROM order_status_history WHERE order_id = ?`, orderID)
	_, _ = db.Exec(`DELETE FROM order_item WHERE order_id = ?`, orderID)
	if _, err := db.Exec(`DELETE FROM `+"`order`"+` WHERE order_id = ?`, orderID); err != nil {
		t.Errorf("cleanup order %d: %v", orderID, err)
	}
}

func uniqueOrderStatusValue() string {
	return fmt.Sprintf("order-status-%d", time.Now().UnixNano())
}
