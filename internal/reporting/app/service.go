package app

import (
	"context"
	"fmt"
	"time"

	"brightbuy-backend/internal/reporting/domain"
)

// Repository is the reporting read port. Implementations must read from reporting views only.
type Repository interface {
	QuarterlySales(ctx context.Context, year int) ([]domain.QuarterlySalesRow, error)
	TopSellingProducts(ctx context.Context, dateRange domain.DateRange, limit int) ([]domain.TopSellingProductRow, error)
	CategoryWiseOrders(ctx context.Context, dateRange *domain.DateRange) ([]domain.CategoryOrderRow, error)
	UpcomingDeliveries(ctx context.Context) ([]domain.UpcomingDeliveryRow, error)
	CustomerOrderPayments(ctx context.Context, dateRange *domain.DateRange, customerID *int64) ([]domain.CustomerOrderPaymentRow, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) QuarterlySales(ctx context.Context, year int) ([]domain.QuarterlySalesRow, error) {
	if year < 2000 || year > 2100 {
		return nil, fmt.Errorf("year must be between 2000 and 2100")
	}
	rows, err := s.repo.QuarterlySales(ctx, year)
	if err != nil {
		return nil, err
	}

	byQuarter := make(map[int]string, len(rows))
	for _, row := range rows {
		byQuarter[row.Quarter] = row.SalesValue
	}
	result := make([]domain.QuarterlySalesRow, 0, 4)
	for quarter := 1; quarter <= 4; quarter++ {
		salesValue := byQuarter[quarter]
		if salesValue == "" {
			salesValue = "0.00"
		}
		result = append(result, domain.QuarterlySalesRow{Year: year, Quarter: quarter, SalesValue: salesValue})
	}
	return result, nil
}

func (s *Service) TopSellingProducts(ctx context.Context, from, to time.Time, limit int) ([]domain.TopSellingProductRow, error) {
	r, err := newDateRange(from, to)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 100 {
		return nil, fmt.Errorf("limit must be between 1 and 100")
	}
	return s.repo.TopSellingProducts(ctx, r, limit)
}

func (s *Service) CategoryWiseOrders(ctx context.Context, from, to *time.Time) ([]domain.CategoryOrderRow, error) {
	var r *domain.DateRange
	if from != nil || to != nil {
		if from == nil || to == nil {
			return nil, fmt.Errorf("from and to must be provided together")
		}
		value, err := newDateRange(*from, *to)
		if err != nil {
			return nil, err
		}
		r = &value
	}
	return s.repo.CategoryWiseOrders(ctx, r)
}

func (s *Service) UpcomingDeliveries(ctx context.Context) ([]domain.UpcomingDeliveryRow, error) {
	return s.repo.UpcomingDeliveries(ctx)
}

func (s *Service) CustomerOrderPayments(ctx context.Context, from, to *time.Time, customerID *int64) ([]domain.CustomerOrderPaymentRow, error) {
	var r *domain.DateRange
	if from != nil || to != nil {
		if from == nil || to == nil {
			return nil, fmt.Errorf("from and to must be provided together")
		}
		value, err := newDateRange(*from, *to)
		if err != nil {
			return nil, err
		}
		r = &value
	}
	if customerID != nil && *customerID <= 0 {
		return nil, fmt.Errorf("customerId must be positive")
	}
	return s.repo.CustomerOrderPayments(ctx, r, customerID)
}

func newDateRange(from, to time.Time) (domain.DateRange, error) {
	from = from.UTC()
	to = to.UTC()
	if to.Before(from) || to.Equal(from) {
		return domain.DateRange{}, fmt.Errorf("to must be after from")
	}
	return domain.DateRange{From: from, To: to}, nil
}
