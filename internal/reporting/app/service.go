package app

import (
	"context"
	"fmt"
	"time"
)

// Repository is the reporting read port. Implementations must read from reporting views only.
type Repository interface {
	QuarterlySales(ctx context.Context, year int) ([]QuarterlySalesRow, error)
	TopSellingProducts(ctx context.Context, dateRange *DateRange, limit int) ([]TopSellingProductRow, error)
	CategoryWiseOrders(ctx context.Context, dateRange *DateRange) ([]CategoryOrderCount, error)
	UpcomingDeliveries(ctx context.Context) ([]UpcomingDeliveryRow, error)
	CustomerOrderPayments(ctx context.Context, dateRange *DateRange, customerID *int64) ([]CustomerOrderPaymentRow, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) QuarterlySales(ctx context.Context, year int) ([]QuarterlySalesRow, error) {
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
	result := make([]QuarterlySalesRow, 0, 4)
	for quarter := 1; quarter <= 4; quarter++ {
		salesValue := byQuarter[quarter]
		if salesValue == "" {
			salesValue = "0.00"
		}
		result = append(result, QuarterlySalesRow{Year: year, Quarter: quarter, SalesValue: salesValue})
	}
	return result, nil
}

func (s *Service) TopSellingProducts(ctx context.Context, from, to *time.Time, limit int) (TopSellingProductsReport, error) {
	if limit <= 0 || limit > 100 {
		return TopSellingProductsReport{}, fmt.Errorf("limit must be between 1 and 100")
	}

	var r *DateRange
	if from != nil || to != nil {
		if from == nil || to == nil {
			return TopSellingProductsReport{}, fmt.Errorf("from and to must be provided together")
		}
		value, err := newDateRange(*from, *to)
		if err != nil {
			return TopSellingProductsReport{}, err
		}
		r = &value
	}

	rows, err := s.repo.TopSellingProducts(ctx, r, limit)
	if err != nil {
		return TopSellingProductsReport{}, err
	}

	return TopSellingProductsReport{
		From:     formatDatePointer(from),
		To:       formatInclusiveEndPointer(to),
		Products: rows,
	}, nil
}

func (s *Service) CategoryWiseOrders(ctx context.Context, from, to *time.Time) (CategoryWiseReport, error) {
	var r *DateRange
	if from != nil || to != nil {
		if from == nil || to == nil {
			return CategoryWiseReport{}, fmt.Errorf("from and to must be provided together")
		}
		value, err := newDateRange(*from, *to)
		if err != nil {
			return CategoryWiseReport{}, err
		}
		r = &value
	}

	rows, err := s.repo.CategoryWiseOrders(ctx, r)
	if err != nil {
		return CategoryWiseReport{}, err
	}

	return CategoryWiseReport{
		Categories: rows,
		Note:       "Counts do not sum to total orders — an order spanning multiple categories is counted once per category.",
	}, nil
}

func (s *Service) UpcomingDeliveries(ctx context.Context) ([]UpcomingDeliveryRow, error) {
	return s.repo.UpcomingDeliveries(ctx)
}

func (s *Service) CustomerOrderPayments(ctx context.Context, from, to *time.Time, customerID *int64) ([]CustomerOrderPaymentRow, error) {
	var r *DateRange
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

func newDateRange(from, to time.Time) (DateRange, error) {
	from = from.UTC()
	to = to.UTC()
	if to.Before(from) || to.Equal(from) {
		return DateRange{}, fmt.Errorf("to must be after from")
	}
	return DateRange{From: from, To: to}, nil
}

func formatDatePointer(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format("2006-01-02")
	return &formatted
}

func formatInclusiveEndPointer(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.AddDate(0, 0, -1).Format("2006-01-02")
	return &formatted
}
