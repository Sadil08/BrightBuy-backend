package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"brightbuy-backend/internal/delivery/domain"
)

type fakeEstimator struct {
	days      int
	mode      domain.DeliveryMode
	cityID    *int
	items     []byte
	callCount int
}

func (f *fakeEstimator) EstimateDays(
	_ context.Context,
	mode domain.DeliveryMode,
	cityID *int,
	itemsJSON []byte,
) (int, error) {
	f.mode = mode
	f.cityID = cityID
	f.items = itemsJSON
	f.callCount++
	return f.days, nil
}

func TestPreview(t *testing.T) {
	estimator := &fakeEstimator{days: 5}
	fixedNow := func() time.Time {
		return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	}

	service := NewService(estimator, fixedNow)

	cityID := 1
	result, err := service.Preview(
		context.Background(),
		domain.StandardDelivery,
		&cityID,
		[]domain.LineInput{
			{VariantID: 10, Quantity: 2},
		},
	)
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}

	if result.EstimatedDays != 5 {
		t.Fatalf("got %d days, want 5", result.EstimatedDays)
	}

	expectedDate := "2026-10-06"
	if result.EstimatedDate.Format("2006-01-02") != expectedDate {
		t.Fatalf(
			"got date %s, want %s",
			result.EstimatedDate.Format("2006-01-02"),
			expectedDate,
		)
	}

	if estimator.callCount != 1 {
		t.Fatalf("estimator called %d times, want 1", estimator.callCount)
	}

	if estimator.cityID == nil || *estimator.cityID != 1 {
		t.Fatalf("wrong city ID passed to estimator")
	}

	var items []domain.LineInput
	if err := json.Unmarshal(estimator.items, &items); err != nil {
		t.Fatalf("invalid items JSON: %v", err)
	}

	if len(items) != 1 || items[0].VariantID != 10 {
		t.Fatalf("wrong items passed to estimator: %+v", items)
	}
}

func TestPreviewStorePickupIgnoresCity(t *testing.T) {
	estimator := &fakeEstimator{days: 0}
	service := NewService(estimator, time.Now)

	cityID := 1

	_, err := service.Preview(
		context.Background(),
		domain.StorePickup,
		&cityID,
		[]domain.LineInput{
			{VariantID: 10, Quantity: 1},
		},
	)
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}

	if estimator.cityID != nil {
		t.Fatalf("Store Pickup should pass nil city ID")
	}
}

func TestPreviewRequiresCityForStandardDelivery(t *testing.T) {
	estimator := &fakeEstimator{days: 5}
	service := NewService(estimator, time.Now)

	_, err := service.Preview(
		context.Background(),
		domain.StandardDelivery,
		nil,
		[]domain.LineInput{
			{VariantID: 10, Quantity: 1},
		},
	)
	if err != ErrInvalidRequest {
		t.Fatalf("got error %v, want ErrInvalidRequest", err)
	}

	if estimator.callCount != 0 {
		t.Fatalf("database estimator should not be called for invalid input")
	}
}

/*This test file verifies that the Delivery Estimation Service.Preview() works correctly
without using the real database. It creates a fakeEstimator that pretends to calculate
delivery days and records what the service sends to it. The first test checks that a Standard
Delivery request correctly returns 5 days, calculates the expected date, calls the estimator
exactly once, passes the correct city ID, and sends the correct cart items as JSON. The second
test verifies that Store Pickup ignores the city ID by passing nil to the estimator. The third
test verifies that Standard Delivery requires a city and that the estimator is not called when

the request is invalid.*/
