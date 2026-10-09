//go:build integration

package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"brightbuy-backend/internal/inventory/app"
	"brightbuy-backend/internal/shared/testdb"
)

var testDB *sql.DB

func TestMain(m *testing.M) {
	db, cleanup, err := testdb.Start(context.Background(), "brightbuy_inventory_test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	testDB = db
	code := m.Run()
	cleanup()
	os.Exit(code)
}

var ctx = context.Background()

func stock(t *testing.T, variantID int) int {
	t.Helper()
	return testdb.Int(t, testDB, `SELECT stock_quantity FROM product_variant WHERE variant_id = ?`, variantID)
}

func movements(t *testing.T, variantID int) int {
	t.Helper()
	return testdb.Int(t, testDB, `SELECT COUNT(*) FROM stock_movement WHERE variant_id = ?`, variantID)
}

// AC-INVENTORY-1 — the staff lookup shows the EXACT quantity, by SKU (exact and prefix) and by name.
func TestSearchBySKUPrefixAndNameShowsExactQuantity(t *testing.T) {
	repo := NewRepository(testDB)
	v := testdb.Variant(t, testDB, "Searchable Gadget", "BB-1001", "9.99", 7)
	testdb.Variant(t, testDB, "Other Thing", "ZZ-9999", "1.00", 3)

	for name, q := range map[string]string{"exact SKU": "BB-1001", "SKU prefix": "BB-10", "product name": "searchable gad"} {
		items, total, err := repo.Search(ctx, q, 1, 20)
		if err != nil || total != 1 || len(items) != 1 {
			t.Fatalf("%s: items=%v total=%d err=%v", name, items, total, err)
		}
		if items[0].VariantID != v || items[0].SKU != "BB-1001" || items[0].StockQuantity != 7 {
			t.Errorf("%s: got %+v, want variant %d with exact stock 7", name, items[0], v)
		}
	}
	if items, _, _ := repo.Search(ctx, "no-such-thing", 1, 20); len(items) != 0 {
		t.Errorf("no match should return nothing, got %v", items)
	}
}

func TestSearchWithoutQueryListsEverythingPaginated(t *testing.T) {
	repo := NewRepository(testDB)
	for i := 0; i < 5; i++ {
		testdb.Variant(t, testDB, fmt.Sprintf("Page Item %d", i), fmt.Sprintf("PG-%d", i), "1.00", i)
	}
	items, total, err := repo.Search(ctx, "", 1, 2)
	if err != nil || len(items) != 2 || total < 5 {
		t.Fatalf("page 1: items=%d total=%d err=%v", len(items), total, err)
	}
	next, _, _ := repo.Search(ctx, "", 2, 2)
	if len(next) != 2 || next[0].VariantID == items[0].VariantID {
		t.Errorf("page 2 should be a different slice: %v vs %v", next, items)
	}
}

// AC-INVENTORY-2 — +20 on 10 gives 30 and an audit row with the quantity change, reason and actor.
func TestAdjustRestockUpdatesStockAndWritesAuditRow(t *testing.T) {
	repo := NewRepository(testDB)
	staff := testdb.StaffUser(t, testDB, "wh1@example.test", "WAREHOUSE_STAFF")
	v := testdb.Variant(t, testDB, "Restock Item", "RS-1", "5.00", 10)

	if err := repo.Adjust(ctx, staff, v, 20, "restock"); err != nil {
		t.Fatal(err)
	}
	if got := stock(t, v); got != 30 {
		t.Fatalf("stock = %d, want 30", got)
	}
	var change int
	var reason string
	var actor, order sql.NullInt64
	if err := testDB.QueryRow(`SELECT change_qty, reason, acting_user_id, related_order_id FROM stock_movement WHERE variant_id = ?`, v).Scan(&change, &reason, &actor, &order); err != nil {
		t.Fatal(err)
	}
	if change != 20 || reason != "restock" || !actor.Valid || int(actor.Int64) != staff || order.Valid {
		t.Errorf("movement = change %d reason %q actor %v order %v; want 20, restock, staff %d, no order", change, reason, actor, order, staff)
	}
}

// AC-INVENTORY-3 — would-go-negative is rejected, quantity unchanged, NO movement row written.
func TestAdjustBelowZeroIsRejectedWithoutSideEffects(t *testing.T) {
	repo := NewRepository(testDB)
	staff := testdb.StaffUser(t, testDB, "wh2@example.test", "WAREHOUSE_STAFF")
	v := testdb.Variant(t, testDB, "Scarce Item", "SC-1", "5.00", 3)

	err := repo.Adjust(ctx, staff, v, -5, "correction")
	if !errors.Is(err, app.ErrAdjustmentBelowZero) {
		t.Fatalf("error = %v, want ErrAdjustmentBelowZero", err)
	}
	if got := stock(t, v); got != 3 {
		t.Errorf("stock = %d, want 3 (unchanged)", got)
	}
	if got := movements(t, v); got != 0 {
		t.Errorf("movement rows = %d, want 0 for a rejected attempt", got)
	}
	// exactly down to zero is allowed
	if err := repo.Adjust(ctx, staff, v, -3, "correction"); err != nil || stock(t, v) != 0 {
		t.Errorf("adjusting to exactly zero: err=%v stock=%d", err, stock(t, v))
	}
}

func TestAdjustUnknownVariantAndEdgeCases(t *testing.T) {
	repo := NewRepository(testDB)
	staff := testdb.StaffUser(t, testDB, "wh3@example.test", "WAREHOUSE_STAFF")

	if err := repo.Adjust(ctx, staff, 987654, 5, "x"); !errors.Is(err, app.ErrVariantNotFound) {
		t.Errorf("unknown variant: %v, want ErrVariantNotFound", err)
	}

	v := testdb.Variant(t, testDB, "Edge Item", "EDGE-1", "5.00", 4)
	if err := repo.Adjust(ctx, staff, v, 0, "recount, no change"); err != nil { // plan §6: delta 0 is a logged no-op
		t.Errorf("delta 0: %v", err)
	}
	if stock(t, v) != 4 || movements(t, v) != 1 {
		t.Errorf("delta 0: stock=%d rows=%d, want 4 and 1 logged row", stock(t, v), movements(t, v))
	}

	// plan §6: a deactivated variant can still have its stock corrected
	testDB.Exec(`UPDATE product_variant SET is_active = FALSE WHERE variant_id = ?`, v)
	if err := repo.Adjust(ctx, staff, v, 6, "correct before reactivating"); err != nil || stock(t, v) != 10 {
		t.Errorf("deactivated variant: err=%v stock=%d, want allowed and 10", err, stock(t, v))
	}
}

// AC-INVENTORY-4 — an order-driven decrement and a later manual +5 are both in the history and are
// told apart by related_order_id (set vs NULL) and acting_user_id (NULL vs set).
func TestOrderDrivenAndManualMovementsAreDistinguishable(t *testing.T) {
	repo := NewRepository(testDB)
	staff := testdb.StaffUser(t, testDB, "wh4@example.test", "WAREHOUSE_STAFF")
	_, customerID := testdb.Customer(t, testDB, "ac4@example.test")
	v := testdb.Variant(t, testDB, "History Item", "HI-1", "5.00", 10)

	orderID := testdb.PlaceOrder(t, testDB, customerID, fmt.Sprintf(`[{"variant_id":%d,"quantity":2}]`, v), "StorePickup", 0, "ac4-order")
	if err := repo.Adjust(ctx, staff, v, 5, "restock"); err != nil {
		t.Fatal(err)
	}
	if got := stock(t, v); got != 13 { // 10 - 2 + 5
		t.Fatalf("stock = %d, want 13", got)
	}

	rows, err := testDB.Query(`SELECT change_qty, related_order_id, acting_user_id FROM stock_movement WHERE variant_id = ? ORDER BY stock_movement_id`, v)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		change int
		order  sql.NullInt64
		actor  sql.NullInt64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.change, &r.order, &r.actor); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("history rows = %d, want 2: %+v", len(got), got)
	}
	if got[0].change != -2 || !got[0].order.Valid || int(got[0].order.Int64) != orderID || got[0].actor.Valid {
		t.Errorf("order-driven row = %+v, want -2, order %d, no actor", got[0], orderID)
	}
	if got[1].change != 5 || got[1].order.Valid || !got[1].actor.Valid || int(got[1].actor.Int64) != staff {
		t.Errorf("manual row = %+v, want +5, no order, actor %d", got[1], staff)
	}
}

// Concurrent adjustments must not lose updates: sp_adjust_stock locks the row (SELECT ... FOR UPDATE).
func TestConcurrentAdjustmentsDoNotLoseUpdates(t *testing.T) {
	repo := NewRepository(testDB)
	staff := testdb.StaffUser(t, testDB, "wh5@example.test", "WAREHOUSE_STAFF")
	v := testdb.Variant(t, testDB, "Race Item", "RACE-1", "5.00", 0)

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.Adjust(ctx, staff, v, 1, "concurrent restock")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent adjust failed: %v", err)
		}
	}
	if got := stock(t, v); got != n {
		t.Errorf("stock = %d after %d concurrent +1, want %d (lost update)", got, n, n)
	}
	if got := movements(t, v); got != n {
		t.Errorf("movement rows = %d, want %d", got, n)
	}
}

// The trigger is the backstop even for writes that bypass sp_adjust_stock.
func TestTriggerBlocksNegativeStockOutsideTheProcedure(t *testing.T) {
	v := testdb.Variant(t, testDB, "Trigger Item", "TRG-1", "5.00", 1)
	if _, err := testDB.Exec(`UPDATE product_variant SET stock_quantity = -1 WHERE variant_id = ?`, v); err == nil {
		t.Fatal("direct UPDATE to a negative quantity should be rejected by trg_stock_nonnegative")
	}
}
