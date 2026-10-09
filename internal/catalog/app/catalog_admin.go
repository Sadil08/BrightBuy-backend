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

type Repository interface {
	CreateProduct(context.Context, domain.Product, []int64, []VariantInput) (domain.Product, error)
	UpdateProduct(context.Context, int64, ProductPatch) error
	SetProductActive(context.Context, int64, bool) error
	CreateVariant(context.Context, int64, VariantInput) (domain.Variant, error)
	UpdateVariant(context.Context, int64, VariantPatch) error
	SetVariantActive(context.Context, int64, bool) error
	CreateCategory(context.Context, CategoryInput) (domain.Category, error)
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

func (s *CatalogAdminService) CreateProduct(ctx context.Context, input ProductInput) (domain.Product, error) {
	if err := validateProduct(input); err != nil {
		return domain.Product{}, err
	}
	ok, err := s.repo.ActiveCategoriesExist(ctx, input.CategoryIDs)
	if err != nil {
		return domain.Product{}, err
	}
	if !ok {
		return domain.Product{}, domain.ErrCategoryInactive
	}

	product := domain.Product{Name: strings.TrimSpace(input.Name), Description: input.Description, Active: true}
	var created domain.Product
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

func (s *CatalogAdminService) CreateVariant(ctx context.Context, productID int64, input VariantInput) (domain.Variant, error) {
	if productID <= 0 {
		return domain.Variant{}, fmt.Errorf("%w: product ID must be positive", domain.ErrValidation)
	}
	if err := validateVariant(input); err != nil {
		return domain.Variant{}, err
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

func (s *CatalogAdminService) CreateCategory(ctx context.Context, input CategoryInput) (domain.Category, error) {
	if strings.TrimSpace(input.Name) == "" {
		return domain.Category{}, fmt.Errorf("%w: category name is required", domain.ErrValidation)
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
