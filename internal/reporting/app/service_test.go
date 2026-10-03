package app

import (
	"context"
	"testing"
	"time"
)

type fakeRepository struct {
	quarterlyRows []QuarterlySalesRow
	quarterlyYear int
}

func (f *fakeRepository) QuarterlySales(_ context.Context, year int) ([]QuarterlySalesRow, error) {
	f.quarterlyYear = year
	return f.quarterlyRows, nil
}
func (f *fakeRepository) TopSellingProducts(_ context.Context, _ *DateRange, _ int) ([]TopSellingProductRow, error) {
	return nil, nil
}
func (f *fakeRepository) CategoryWiseOrders(_ context.Context, _ *DateRange) ([]CategoryOrderCount, error) {
	return nil, nil
}
func (f *fakeRepository) UpcomingDeliveries(_ context.Context) ([]UpcomingDeliveryRow, error) {
	return nil, nil
}
func (f *fakeRepository) CustomerOrderPayments(_ context.Context, _ *DateRange, _ *int64) ([]CustomerOrderPaymentRow, error) {
	return nil, nil
}

func TestQuarterlySalesAlwaysReturnsFourQuarters(t *testing.T) {
	repo := &fakeRepository{quarterlyRows: []QuarterlySalesRow{{Year: 2026, Quarter: 1, SalesValue: "100.00"}, {Year: 2026, Quarter: 3, SalesValue: "250.50"}}}
	service := NewService(repo)
	rows, err := service.QuarterlySales(context.Background(), 2026)
	if err != nil {
		t.Fatalf("QuarterlySales() error = %v", err)
	}
	if repo.quarterlyYear != 2026 {
		t.Fatalf("repository year = %d, want 2026", repo.quarterlyYear)
	}
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}
	if rows[0].SalesValue != "100.00" || rows[1].SalesValue != "0.00" || rows[2].SalesValue != "250.50" || rows[3].SalesValue != "0.00" {
		t.Fatalf("unexpected quarterly values: %#v", rows)
	}
}

func TestTopSellingProductsDefaultsToAllTime(t *testing.T) {
	service := NewService(&fakeRepository{})
	report, err := service.TopSellingProducts(context.Background(), nil, nil, 20)
	if err != nil {
		t.Fatalf("TopSellingProducts() error = %v", err)
	}
	if report.From != nil || report.To != nil {
		t.Fatalf("expected nil date range for all-time report: %#v", report)
	}
}

func TestServiceRejectsInvalidDateRange(t *testing.T) {
	service := NewService(&fakeRepository{})
	from := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	to := from
	_, err := service.TopSellingProducts(context.Background(), &from, &to, 10)
	if err == nil {
		t.Fatal("TopSellingProducts() error = nil, want validation error")
	}
}
