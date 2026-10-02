package app

import (
	"brightbuy-backend/internal/cart/domain"
	"brightbuy-backend/internal/cart/mysql"
	"context"
	"fmt"
)

// Service contains the cart business logic.
// It validates requests and orchestrates repository calls.
type Service struct {
	repo *mysql.CartRepository
}

func NewService(repo *mysql.CartRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetCart(ctx context.Context, customerID int) (*domain.Cart, error) {
	if customerID <= 0 {
		return nil, fmt.Errorf("customer ID must be positive")
	}

	return s.repo.Get(ctx, customerID)
}

func (s *Service) AddItem(
	ctx context.Context,
	customerID, variantID, quantity int,
) (*domain.Cart, error) {
	if customerID <= 0 || variantID <= 0 {
		return nil, fmt.Errorf("customer ID and variant ID must be positive")
	}
	if quantity < 1 {
		return nil, fmt.Errorf("quantity must be at least 1")
	}

	if err := s.validateStock(ctx, variantID, quantity); err != nil {
		return nil, err
	}

	return s.repo.UpsertLine(ctx, customerID, variantID, quantity)
}

func (s *Service) UpdateItemQuantity(
	ctx context.Context,
	customerID, variantID, quantity int,
) (*domain.Cart, error) {
	if customerID <= 0 || variantID <= 0 {
		return nil, fmt.Errorf("customer ID and variant ID must be positive")
	}
	if quantity < 1 {
		return nil, fmt.Errorf("quantity must be at least 1")
	}

	existing, err := s.repo.FindLine(ctx, customerID, variantID)
	if err != nil {
		return nil, fmt.Errorf("find cart item: %w", err)
	}
	if existing == nil {
		return nil, fmt.Errorf("cart item not found")
	}

	if err := s.validateStock(ctx, variantID, quantity); err != nil {
		return nil, err
	}

	return s.repo.UpsertLine(ctx, customerID, variantID, quantity)
}

func (s *Service) validateStock(
	ctx context.Context,
	variantID, requestedQty int,
) error {
	stock, err := s.repo.VariantStock(ctx, variantID)
	if err != nil {
		return err
	}

	if requestedQty > stock {
		return fmt.Errorf(
			"requested quantity %d exceeds available stock %d",
			requestedQty,
			stock,
		)
	}

	return nil
}

func (s *Service) RemoveItem(
	ctx context.Context,
	customerID, variantID int,
) (*domain.Cart, error) {
	if customerID <= 0 || variantID <= 0 {
		return nil, fmt.Errorf("customer ID and variant ID must be positive")
	}

	if err := s.repo.DeleteLine(ctx, customerID, variantID); err != nil {
		return nil, fmt.Errorf("delete cart item: %w", err)
	}

	return s.repo.Get(ctx, customerID)
}
