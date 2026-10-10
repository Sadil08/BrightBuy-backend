package app

import (
	"context"
	"fmt"
	"strings"

	"brightbuy-backend/internal/catalog/domain"
)

type VariantInput struct {
	SKU           string `json:"sku"`
	PriceCents    int64  `json:"price_cents"`
	StockQuantity int    `json:"stock_quantity"`
}

type ProductInput struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	CategoryIDs []int64        `json:"category_ids"`
	Variants    []VariantInput `json:"variants"`
}

type ProductPatch struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type VariantPatch struct {
	SKU           *string `json:"sku"`
	PriceCents    *int64  `json:"price_cents"`
	StockQuantity *int    `json:"stock_quantity"`
}

type CategoryInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type CategoryPatch struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type AdminProduct struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type AdminVariant struct {
	ID            int64  `json:"id"`
	ProductID     int64  `json:"product_id"`
	SKU           string `json:"sku"`
	PriceCents    int64  `json:"price_cents"`
	StockQuantity int    `json:"stock_quantity"`
	Active        bool   `json:"active"`
}

type AdminCategory struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type Repository interface {
	CreateProduct(context.Context, AdminProduct, []int64, []VariantInput) (AdminProduct, error)
	UpdateProduct(context.Context, int64, ProductPatch) error
	SetProductActive(context.Context, int64, bool) error
	CreateVariant(context.Context, int64, VariantInput) (AdminVariant, error)
	UpdateVariant(context.Context, int64, VariantPatch) error
	SetVariantActive(context.Context, int64, bool) error
	CreateCategory(context.Context, CategoryInput) (AdminCategory, error)
	UpdateCategory(context.Context, int64, CategoryPatch) error
	SetCategoryActive(context.Context, int64, bool) error
	ActiveCategoriesExist(context.Context, []int64) (bool, error)
}

type Transactioner interface {
	WithinTransaction(context.Context, func(Repository) error) error
}

type CatalogAdminService struct {
	repo Repository
	tx   Transactioner
}

func NewCatalogAdminService(repo Repository, tx Transactioner) *CatalogAdminService {
	return &CatalogAdminService{repo: repo, tx: tx}
}

func (s *CatalogAdminService) CreateProduct(ctx context.Context, input ProductInput) (AdminProduct, error) {
	if err := validateProduct(input); err != nil {
		return AdminProduct{}, err
	}
	ok, err := s.repo.ActiveCategoriesExist(ctx, input.CategoryIDs)
	if err != nil {
		return AdminProduct{}, err
	}
	if !ok {
		return AdminProduct{}, domain.ErrCategoryInactive
	}

	product := AdminProduct{Name: strings.TrimSpace(input.Name), Description: input.Description, Active: true}
	var created AdminProduct
	operation := func(repo Repository) error {
		var err error
		created, err = repo.CreateProduct(ctx, product, input.CategoryIDs, input.Variants)
		return err
	}
	if s.tx != nil {
		err = s.tx.WithinTransaction(ctx, operation)
	} else {
		err = operation(s.repo)
	}
	return created, err
}

func (s *CatalogAdminService) UpdateProduct(ctx context.Context, id int64, patch ProductPatch) error {
	if id <= 0 || patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return fmt.Errorf("%w: product fields are invalid", domain.ErrValidation)
	}
	return s.repo.UpdateProduct(ctx, id, patch)
}

func (s *CatalogAdminService) DeactivateProduct(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: product ID must be positive", domain.ErrValidation)
	}
	return s.repo.SetProductActive(ctx, id, false)
}

func (s *CatalogAdminService) CreateVariant(ctx context.Context, productID int64, input VariantInput) (AdminVariant, error) {
	if productID <= 0 {
		return AdminVariant{}, fmt.Errorf("%w: product ID must be positive", domain.ErrValidation)
	}
	if err := validateVariant(input); err != nil {
		return AdminVariant{}, err
	}
	return s.repo.CreateVariant(ctx, productID, input)
}

func (s *CatalogAdminService) UpdateVariant(ctx context.Context, id int64, patch VariantPatch) error {
	if id <= 0 {
		return fmt.Errorf("%w: variant ID must be positive", domain.ErrValidation)
	}
	if patch.PriceCents != nil && *patch.PriceCents < 0 || patch.StockQuantity != nil && *patch.StockQuantity < 0 {
		return fmt.Errorf("%w: variant values are invalid", domain.ErrValidation)
	}
	if patch.SKU != nil && strings.TrimSpace(*patch.SKU) == "" {
		return fmt.Errorf("%w: SKU is required", domain.ErrValidation)
	}
	return s.repo.UpdateVariant(ctx, id, patch)
}

func (s *CatalogAdminService) DeactivateVariant(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: variant ID must be positive", domain.ErrValidation)
	}
	return s.repo.SetVariantActive(ctx, id, false)
}

func (s *CatalogAdminService) CreateCategory(ctx context.Context, input CategoryInput) (AdminCategory, error) {
	if strings.TrimSpace(input.Name) == "" {
		return AdminCategory{}, fmt.Errorf("%w: category name is required", domain.ErrValidation)
	}
	return s.repo.CreateCategory(ctx, input)
}

func (s *CatalogAdminService) UpdateCategory(ctx context.Context, id int64, patch CategoryPatch) error {
	if id <= 0 || patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return fmt.Errorf("%w: category fields are invalid", domain.ErrValidation)
	}
	return s.repo.UpdateCategory(ctx, id, patch)
}

func (s *CatalogAdminService) DeactivateCategory(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("%w: category ID must be positive", domain.ErrValidation)
	}
	return s.repo.SetCategoryActive(ctx, id, false)
}

func validateProduct(input ProductInput) error {
	if strings.TrimSpace(input.Name) == "" || len(input.Variants) == 0 {
		return fmt.Errorf("%w: product name and at least one variant are required", domain.ErrValidation)
	}
	if len(input.CategoryIDs) == 0 {
		return fmt.Errorf("%w: at least one category is required", domain.ErrValidation)
	}
	for _, variant := range input.Variants {
		if err := validateVariant(variant); err != nil {
			return err
		}
	}
	return nil
}

func validateVariant(input VariantInput) error {
	if strings.TrimSpace(input.SKU) == "" || input.PriceCents < 0 || input.StockQuantity < 0 {
		return fmt.Errorf("%w: SKU, price, and stock are invalid", domain.ErrValidation)
	}
	return nil
}
