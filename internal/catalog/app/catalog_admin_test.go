package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/catalog/domain"
)

type fakeRepository struct {
	createdProduct bool
	createdResult  domain.Product
	activeCats     bool
	deactivated    bool
	deactivatedID  int64
	deactivatedOn  bool
	repositoryErr  error

	updatedProductID  int64
	updatedVariantID  int64
	updatedCategoryID int64

	updatedProduct  bool
	updatedVariant  bool
	updatedCategory bool
}

func (f *fakeRepository) CreateProduct(context.Context, domain.Product, []int64, []VariantInput) (domain.Product, error) {
	f.createdProduct = true
	if f.createdResult.ID != 0 {
		return f.createdResult, f.repositoryErr
	}
	return domain.Product{ID: 1, Active: true}, f.repositoryErr
}
func (f *fakeRepository) UpdateProduct(_ context.Context, id int64, _ ProductPatch) error {
	f.updatedProduct = true
	f.updatedProductID = id
	return f.repositoryErr
}
func (f *fakeRepository) SetProductActive(_ context.Context, id int64, active bool) error {
	f.deactivated = true
	f.deactivatedID = id
	f.deactivatedOn = active
	return f.repositoryErr
}
func (f *fakeRepository) CreateVariant(context.Context, int64, VariantInput) (domain.Variant, error) {
	return domain.Variant{}, f.repositoryErr
}
func (f *fakeRepository) UpdateVariant(_ context.Context, id int64, _ VariantPatch) error {
	f.updatedVariant = true
	f.updatedVariantID = id
	return f.repositoryErr
}
func (f *fakeRepository) SetVariantActive(_ context.Context, id int64, active bool) error {
	f.deactivated = true
	f.deactivatedID = id
	f.deactivatedOn = active
	return f.repositoryErr
}
func (f *fakeRepository) CreateCategory(context.Context, CategoryInput) (domain.Category, error) {
	return domain.Category{}, nil
}
func (f *fakeRepository) UpdateCategory(_ context.Context, id int64, _ CategoryPatch) error {
	f.updatedCategory = true
	f.updatedCategoryID = id
	return f.repositoryErr
}
func (f *fakeRepository) SetCategoryActive(_ context.Context, id int64, active bool) error {
	f.deactivated = true
	f.deactivatedID = id
	f.deactivatedOn = active
	return f.repositoryErr
}
func (f *fakeRepository) ActiveCategoriesExist(context.Context, []int64) (bool, error) {
	return f.activeCats, nil
}

func TestCreateProductRequiresActiveCategoriesAndVariant(t *testing.T) {
	repo := &fakeRepository{activeCats: true}
	service := NewCatalogAdminService(repo, nil)

	_, err := service.CreateProduct(context.Background(), ProductInput{Name: "Phone", CategoryIDs: []int64{1}})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	_, err = service.CreateProduct(context.Background(), ProductInput{Name: "Phone", CategoryIDs: []int64{1}, Variants: []VariantInput{{SKU: "PHONE-1", PriceCents: 100}}})
	if err != nil || !repo.createdProduct {
		t.Fatalf("expected product creation, error=%v created=%v", err, repo.createdProduct)
	}
}

func TestDeactivateProductOnlySetsInactive(t *testing.T) {
	repo := &fakeRepository{}
	service := NewCatalogAdminService(repo, nil)
	if err := service.DeactivateProduct(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if !repo.deactivated {
		t.Fatal("expected repository active flag update")
	}
	if repo.deactivatedOn {
		t.Fatal("expected product deactivation to pass false")
	}
}

func TestCreateProductRejectsInactiveCategory(t *testing.T) {
	service := NewCatalogAdminService(&fakeRepository{}, nil)
	_, err := service.CreateProduct(context.Background(), ProductInput{
		Name:        "Phone",
		CategoryIDs: []int64{1},
		Variants:    []VariantInput{{SKU: "PHONE-1", PriceCents: 100}},
	})
	if !errors.Is(err, domain.ErrCategoryInactive) {
		t.Fatalf("expected inactive category error, got %v", err)
	}
}

func TestVariantAndCategoryValidation(t *testing.T) {
	service := NewCatalogAdminService(&fakeRepository{}, nil)
	_, err := service.CreateVariant(context.Background(), 1, VariantInput{SKU: "", PriceCents: 1})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected variant validation error, got %v", err)
	}
	_, err = service.CreateCategory(context.Background(), CategoryInput{})
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("expected category validation error, got %v", err)
	}
}

func TestDeactivateVariantAndCategoryUseSoftDelete(t *testing.T) {
	repo := &fakeRepository{}
	service := NewCatalogAdminService(repo, nil)
	if err := service.DeactivateVariant(context.Background(), 11); err != nil {
		t.Fatal(err)
	}
	if !repo.deactivated || repo.deactivatedID != 11 {
		t.Fatalf("expected variant deactivation, got active=%v id=%d", repo.deactivated, repo.deactivatedID)
	}
	repo.deactivated = false
	if err := service.DeactivateCategory(context.Background(), 12); err != nil {
		t.Fatal(err)
	}
	if !repo.deactivated || repo.deactivatedID != 12 {
		t.Fatalf("expected category deactivation, got active=%v id=%d", repo.deactivated, repo.deactivatedID)
	}
	if repo.deactivatedOn {
		t.Fatal("expected category deactivation to pass false")
	}
}

type fakeTransactioner struct {
	called bool
}

func (f *fakeTransactioner) WithinTransaction(ctx context.Context, fn func(Repository) error) error {
	f.called = true
	return fn(&fakeRepository{activeCats: true})
}

func TestCreateProductDelegatesToTransactioner(t *testing.T) {
	tx := &fakeTransactioner{}
	service := NewCatalogAdminService(&fakeRepository{activeCats: true}, tx)
	_, err := service.CreateProduct(context.Background(), ProductInput{
		Name:        "Phone",
		CategoryIDs: []int64{1},
		Variants:    []VariantInput{{SKU: "PHONE-1", PriceCents: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tx.called {
		t.Fatal("expected product creation to use the transactioner")
	}
}

func TestCreateProductReturnsRepositoryProduct(t *testing.T) {
	repo := &fakeRepository{activeCats: true, createdResult: domain.Product{ID: 42, Name: "Created", Active: true}}
	service := NewCatalogAdminService(repo, nil)
	created, err := service.CreateProduct(context.Background(), ProductInput{
		Name:        "Input",
		CategoryIDs: []int64{1},
		Variants:    []VariantInput{{SKU: "INPUT-1", PriceCents: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 42 || created.Name != "Created" {
		t.Fatalf("expected repository product, got %+v", created)
	}
}

func TestUpdateMethodsForwardIDs(t *testing.T) {
	repo := &fakeRepository{}
	service := NewCatalogAdminService(repo, nil)
	name := "Updated"
	price := int64(250)
	stock := 4
	if err := service.UpdateProduct(context.Background(), 10, ProductPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateVariant(context.Background(), 11, VariantPatch{PriceCents: &price, StockQuantity: &stock}); err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateCategory(context.Background(), 12, CategoryPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if !repo.updatedProduct || repo.updatedProductID != 10 || !repo.updatedVariant || repo.updatedVariantID != 11 || !repo.updatedCategory || repo.updatedCategoryID != 12 {
		t.Fatalf("update calls not forwarded correctly: %+v", repo)
	}
}

func TestUpdateAndCreateMethodsPropagateRepositoryErrors(t *testing.T) {
	wanted := errors.New("repository failed")
	repo := &fakeRepository{activeCats: true, repositoryErr: wanted}
	service := NewCatalogAdminService(repo, nil)
	name := "Updated"
	price := int64(250)
	stock := 4
	cases := []struct {
		name string
		call func() error
	}{
		{"product update", func() error { return service.UpdateProduct(context.Background(), 1, ProductPatch{Name: &name}) }},
		{"variant update", func() error {
			return service.UpdateVariant(context.Background(), 1, VariantPatch{PriceCents: &price, StockQuantity: &stock})
		}},
		{"category update", func() error { return service.UpdateCategory(context.Background(), 1, CategoryPatch{Name: &name}) }},
		{"product deactivate", func() error { return service.DeactivateProduct(context.Background(), 1) }},
		{"variant deactivate", func() error { return service.DeactivateVariant(context.Background(), 1) }},
		{"category deactivate", func() error { return service.DeactivateCategory(context.Background(), 1) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if !errors.Is(testCase.call(), wanted) {
				t.Fatalf("expected repository error %v", wanted)
			}
		})
	}
}

func TestInvalidIDsAndVariantPatchValuesAreRejected(t *testing.T) {
	repo := &fakeRepository{}
	service := NewCatalogAdminService(repo, nil)
	negativePrice := int64(-1)
	negativeStock := -1
	emptySKU := ""
	cases := []struct {
		name string
		call func() error
	}{
		{"product update ID", func() error { return service.UpdateProduct(context.Background(), 0, ProductPatch{}) }},
		{"product deactivate ID", func() error { return service.DeactivateProduct(context.Background(), 0) }},
		{"variant create product ID", func() error {
			_, err := service.CreateVariant(context.Background(), 0, VariantInput{SKU: "SKU", PriceCents: 1})
			return err
		}},
		{"variant update ID", func() error { return service.UpdateVariant(context.Background(), 0, VariantPatch{}) }},
		{"variant deactivate ID", func() error { return service.DeactivateVariant(context.Background(), 0) }},
		{"category update ID", func() error { return service.UpdateCategory(context.Background(), 0, CategoryPatch{}) }},
		{"category deactivate ID", func() error { return service.DeactivateCategory(context.Background(), 0) }},
		{"negative price", func() error {
			return service.UpdateVariant(context.Background(), 1, VariantPatch{PriceCents: &negativePrice})
		}},
		{"negative stock", func() error {
			return service.UpdateVariant(context.Background(), 1, VariantPatch{StockQuantity: &negativeStock})
		}},
		{"empty SKU", func() error { return service.UpdateVariant(context.Background(), 1, VariantPatch{SKU: &emptySKU}) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if !errors.Is(testCase.call(), domain.ErrValidation) {
				t.Fatalf("expected validation error")
			}
		})
	}
}
