package app

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"brightbuy-backend/internal/delivery/domain"
)

var ErrInvalidRequest = errors.New("invalid delivery estimate request")

type Estimator interface {
	EstimateDays(
		ctx context.Context,
		mode domain.DeliveryMode,
		cityID *int,
		itemsJSON []byte,
	) (int, error)
}

type Clock func() time.Time

type Service struct {
	estimator Estimator
	now       Clock
}

func NewService(estimator Estimator, now Clock) *Service {
	if now == nil {
		now = time.Now
	}

	return &Service{
		estimator: estimator,
		now:       now,
	}
}

func (s *Service) Preview(
	ctx context.Context,
	mode domain.DeliveryMode,
	cityID *int,
	items []domain.LineInput,
) (domain.Estimate, error) {
	if !mode.Valid() {
		return domain.Estimate{}, ErrInvalidRequest
	}

	if mode == domain.StandardDelivery && cityID == nil {
		return domain.Estimate{}, ErrInvalidRequest
	}

	if mode == domain.StorePickup {
		cityID = nil
	}

	for _, item := range items {
		if item.VariantID <= 0 || item.Quantity <= 0 {
			return domain.Estimate{}, ErrInvalidRequest
		}
	}

	itemsJSON, err := json.Marshal(items)
	if err != nil {
		return domain.Estimate{}, err
	}

	days, err := s.estimator.EstimateDays(
		ctx,
		mode,
		cityID,
		itemsJSON,
	)
	if err != nil {
		return domain.Estimate{}, err
	}

	now := s.now().UTC()

	return domain.Estimate{
		Mode:          mode,
		EstimatedDays: days,
		EstimatedDate: now.AddDate(0, 0, days),
	}, nil
}

/*I'm implementing a public POST /delivery/estimate preview API that takes
the customer's delivery mode, city, and cart items, calls the same
fn_estimate_delivery_days database function used by checkout, and returns
 the estimated number of calendar days and delivery date before the customer
 places the order.*/
