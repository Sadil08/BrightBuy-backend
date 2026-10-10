//go:build integration

package mysql

import (
	"context"
	"os"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

func TestReportingViewsCanBeQueried(t *testing.T) {
	dsn := os.Getenv("REPORTING_DB_DSN")
	if dsn == "" {
		t.Skip("REPORTING_DB_DSN is not set")
	}

	repo, err := NewRepository(dsn)
	if err != nil {
		t.Fatalf("open reporting repository: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	if _, err := repo.QuarterlySales(ctx, 2026); err != nil {
		t.Fatalf("quarterly sales view query failed: %v", err)
	}
	if _, err := repo.TopSellingProducts(ctx, nil, 20); err != nil {
		t.Fatalf("top-selling-products view query failed: %v", err)
	}
	if _, err := repo.CategoryWiseOrders(ctx, nil); err != nil {
		t.Fatalf("category-wise-orders view query failed: %v", err)
	}
	if _, err := repo.UpcomingDeliveries(ctx); err != nil {
		t.Fatalf("upcoming-deliveries view query failed: %v", err)
	}
	if _, err := repo.CustomerOrderPayments(ctx, nil, nil); err != nil {
		t.Fatalf("customer-order-summary view query failed: %v", err)
	}
}

func TestReportingCredentialCannotReadBaseTable(t *testing.T) {
	dsn := os.Getenv("REPORTING_DB_DSN")
	if dsn == "" {
		t.Skip("REPORTING_DB_DSN is not set")
	}
	repo, err := NewRepository(dsn)
	if err != nil {
		t.Fatalf("open reporting repository: %v", err)
	}
	defer repo.Close()

	var one int
	err = repo.db.QueryRowContext(context.Background(), "SELECT 1 FROM `order` LIMIT 1").Scan(&one)
	if err == nil {
		t.Fatal("expected reporting credential to be denied access to base table `order`")
	}
}
