package app

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"brightbuy-backend/internal/cart/mysql"
)

func TestService_AddItem_RejectsOverStock(t *testing.T) {
	ctx := context.Background()

	dsn := "brightbuy_app:devapppass@tcp(127.0.0.1:3308)/brightbuy?parseTime=true"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("database ping failed: %v", err)
	}

	repo := mysql.NewCartRepository(db)
	svc := NewService(repo)

	_, err = svc.AddItem(ctx, 1, 42, 9999)
	if err == nil {
		t.Fatal("expected stock validation error, got nil")
	}

	if !strings.Contains(err.Error(), "exceeds available stock") {
		t.Fatalf("unexpected error: %v", err)
	}
}
