package app

import (
	"context"
	"errors"
	"strings"

	"brightbuy-backend/internal/inventory/domain"
)

const (
	// MaxAdjustment bounds one adjustment's size so a typo (or hostile input) can't overflow the INT
	// stock column inside sp_adjust_stock and surface as a 500.
	MaxAdjustment = 1_000_000
	// maxQueryLength bounds the search text; SKUs and product names are far shorter than this.
	maxQueryLength = 100
)

var (
	ErrInvalidRequest      = errors.New("invalid inventory request")
	ErrAdjustmentBelowZero = errors.New("adjustment would take stock below zero")
	ErrVariantNotFound     = errors.New("variant not found")
)

type Repository interface {
	Search(
		ctx context.Context,
		query string,
		page int,
		size int,
	) ([]domain.VariantStock, int, error)

	Adjust(
		ctx context.Context,
		actingUserID int,
		variantID int,
		delta int,
		reason string,
	) error
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (s *Service) Search(
	ctx context.Context,
	query string,
	page int,
	size int,
) ([]domain.VariantStock, int, error) {
	query = strings.TrimSpace(query)
	if page < 1 || size < 1 || size > 100 || len(query) > maxQueryLength {
		return nil, 0, ErrInvalidRequest
	}

	return s.repository.Search(
		ctx,
		query,
		page,
		size,
	)
}

func (s *Service) Adjust(
	ctx context.Context,
	actingUserID int,
	variantID int,
	delta int,
	reason string,
) error {
	reason = strings.TrimSpace(reason)

	if actingUserID <= 0 ||
		variantID <= 0 ||
		reason == "" ||
		len(reason) > 255 ||
		delta > MaxAdjustment ||
		delta < -MaxAdjustment {
		return ErrInvalidRequest
	}

	return s.repository.Adjust(
		ctx,
		actingUserID,
		variantID,
		delta,
		reason,
	)
}

/*Validates page and size.
Trims search text and adjustment reasons.
Rejects invalid users, variants, and reasons.
Allows delta = 0, as required by the plan.
Does not perform stock arithmetic. MySQL owns the atomic update.*/
