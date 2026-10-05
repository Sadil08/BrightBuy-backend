package app

import (
	"context"
	"errors"
	"fmt"

	"brightbuy-backend/internal/cart/domain"
	catalogapp "brightbuy-backend/internal/catalog/app"
)

// maxMergeItems bounds a guest-cart merge so one request can't trigger unbounded writes.
const maxMergeItems = 100

// Store is what the service needs from persistence. *mysql.CartRepository satisfies it; unit tests
// use a fake. Every method that takes customerID is ownership-scoped in its SQL (SEC-CART-1).
type Store interface {
	Get(ctx context.Context, customerID int) (*domain.Cart, error)
	UpsertLine(ctx context.Context, customerID, variantID, quantity int) (*domain.Cart, error)
	FindLine(ctx context.Context, customerID, variantID int) (*domain.CartItem, error)
	FindLineByID(ctx context.Context, customerID, cartItemID int) (*domain.CartItem, error)
	DeleteLine(ctx context.Context, customerID, variantID int) error
	CustomerIDForUser(ctx context.Context, userAccountID int) (int, error)
}

// LineInput is one (variant, quantity) pair from the client. There is deliberately no price field:
// prices are always read from product_variant (SEC-CART-2).
type LineInput struct {
	VariantID int
	Quantity  int
}

// Service contains the cart business logic.
type Service struct {
	repo    Store
	catalog Catalog
}

// Catalog is the subset of catalog business logic that cart needs. It is a separate port so the
// cart service can be unit-tested with a fake instead of a real catalog service.
type Catalog interface {
	GetVariantForCart(ctx context.Context, variantID int) (*catalogapp.CartVariant, error)
}

func NewService(repo Store, catalog Catalog) *Service {
	return &Service{repo: repo, catalog: catalog}
}

// CustomerIDForUser resolves the authenticated user_account_id to the customer who owns the cart.
func (s *Service) CustomerIDForUser(ctx context.Context, userAccountID int) (int, error) {
	return s.repo.CustomerIDForUser(ctx, userAccountID)
}

func (s *Service) GetCart(ctx context.Context, customerID int) (*domain.Cart, error) {
	return s.repo.Get(ctx, customerID)
}

// AddItem makes the line hold exactly `quantity` — it SETS, it does not add to the existing
// quantity (plan.md §6: matches PATCH, avoids "add 2, add 2, got 4" surprises).
func (s *Service) AddItem(ctx context.Context, customerID, variantID, quantity int) (*domain.Cart, error) {
	if quantity < 1 || quantity > domain.MaxLineQuantity {
		return nil, fmt.Errorf("%w: quantity must be between 1 and %d", domain.ErrInvalidInput, domain.MaxLineQuantity)
	}
	if err := s.validateStock(ctx, variantID, quantity); err != nil {
		return nil, err
	}
	return s.repo.UpsertLine(ctx, customerID, variantID, quantity)
}

// UpdateItemQuantity sets the quantity of one of the caller's cart lines. Quantity 0 is the same
// as removing it (plan.md §6 — one code path).
func (s *Service) UpdateItemQuantity(ctx context.Context, customerID, cartItemID, quantity int) (*domain.Cart, error) {
	if quantity < 0 || quantity > domain.MaxLineQuantity {
		return nil, fmt.Errorf("%w: quantity must be between 0 and %d", domain.ErrInvalidInput, domain.MaxLineQuantity)
	}
	line, err := s.repo.FindLineByID(ctx, customerID, cartItemID)
	if err != nil {
		return nil, err
	}
	if line == nil {
		return nil, domain.ErrLineNotFound
	}
	if quantity == 0 {
		return s.removeLine(ctx, customerID, line.VariantID)
	}
	if err := s.validateStock(ctx, line.VariantID, quantity); err != nil {
		return nil, err
	}
	return s.repo.UpsertLine(ctx, customerID, line.VariantID, quantity)
}

// RemoveItem deletes one of the caller's cart lines and returns the remaining cart.
func (s *Service) RemoveItem(ctx context.Context, customerID, cartItemID int) (*domain.Cart, error) {
	line, err := s.repo.FindLineByID(ctx, customerID, cartItemID)
	if err != nil {
		return nil, err
	}
	if line == nil {
		return nil, domain.ErrLineNotFound
	}
	return s.removeLine(ctx, customerID, line.VariantID)
}

func (s *Service) removeLine(ctx context.Context, customerID, variantID int) (*domain.Cart, error) {
	if err := s.repo.DeleteLine(ctx, customerID, variantID); err != nil {
		return nil, fmt.Errorf("delete cart item: %w", err)
	}
	return s.repo.Get(ctx, customerID)
}

// Merge folds a guest's browser cart into the customer's server cart on login (FR-CART-5). Where a
// variant is in both, the HIGHER quantity wins — not the sum (AC-CART-4). Stock is NOT enforced
// here: the cart view flags over-stock lines with stockWarning and checkout is the authoritative
// check, so one stale guest line must not fail the whole merge. Variants that no longer exist or
// are no longer sold are skipped for the same reason.
func (s *Service) Merge(ctx context.Context, customerID int, items []LineInput) (*domain.Cart, error) {
	if len(items) > maxMergeItems {
		return nil, fmt.Errorf("%w: too many items to merge (max %d)", domain.ErrInvalidInput, maxMergeItems)
	}
	for _, in := range items {
		if in.VariantID < 1 || in.Quantity < 1 || in.Quantity > domain.MaxLineQuantity {
			return nil, fmt.Errorf("%w: variantId must be >= 1 and quantity between 1 and %d", domain.ErrInvalidInput, domain.MaxLineQuantity)
		}
	}

	for _, in := range items {
		if _, err := s.catalog.GetVariantForCart(ctx, in.VariantID); err != nil {
			if errors.Is(err, catalogapp.ErrNotFound) {
				continue
			}
			return nil, err
		}
		existing, err := s.repo.FindLine(ctx, customerID, in.VariantID)
		if err != nil {
			return nil, fmt.Errorf("find cart item: %w", err)
		}
		merged := in.Quantity
		if existing != nil && existing.Quantity > merged {
			merged = existing.Quantity
		}
		if _, err := s.repo.UpsertLine(ctx, customerID, in.VariantID, merged); err != nil {
			return nil, err
		}
	}
	return s.repo.Get(ctx, customerID)
}

func (s *Service) validateStock(
	ctx context.Context,
	variantID, requestedQty int,
) error {
	variant, err := s.catalog.GetVariantForCart(ctx, variantID)
	if errors.Is(err, catalogapp.ErrNotFound) {
		return domain.ErrVariantNotFound
	}
	if err != nil {
		return err
	}
	if requestedQty > variant.StockQuantity {
		return &domain.StockExceededError{
			Requested: requestedQty,
			Available: variant.StockQuantity,
		}
	}

	return nil
}
