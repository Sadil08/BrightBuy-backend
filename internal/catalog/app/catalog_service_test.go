package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/catalog/domain"
)

// fakeProductRepository is a hand-written stand-in for mysql.ProductRepository. It satisfies the
// ProductRepository interface purely structurally — nothing declares "fakeProductRepository
// implements ProductRepository" anywhere, the compiler just checks the method set matches wherever
// it's used as one (file 05 in learn-go/ covers this). This is the entire payoff of the port/adapter
// split: CatalogService never needs a real database to be tested.
type fakeProductRepository struct {
	products  []domain.Product
	total     int
	listErr   error
	getByID   map[int]*domain.Product
	getIDErr  error
	lastQuery ListFilter // records the last call, so a test can assert what CatalogService passed down
}

func (f *fakeProductRepository) List(ctx context.Context, filter ListFilter) ([]domain.Product, int, error) {
	f.lastQuery = filter
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.products, f.total, nil
}

func (f *fakeProductRepository) GetByID(ctx context.Context, id int) (*domain.Product, error) {
	if f.getIDErr != nil {
		return nil, f.getIDErr
	}
	p, ok := f.getByID[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}

func TestListProductsDefaultsPageAndSize(t *testing.T) {
	repo := &fakeProductRepository{}
	svc := NewCatalogService(repo, nil)

	// Zero-value ListFilter{} — as if a request came in with no ?page or ?size at all.
	if _, _, err := svc.ListProducts(context.Background(), ListFilter{}); err != nil {
		t.Fatalf("ListProducts: %v", err)
	}

	if repo.lastQuery.Page != 1 {
		t.Errorf("Page = %d, want 1 (default)", repo.lastQuery.Page)
	}
	if repo.lastQuery.PageSize != defaultPageSize {
		t.Errorf("PageSize = %d, want %d (default)", repo.lastQuery.PageSize, defaultPageSize)
	}
}

func TestListProductsClampsOversizedPageSize(t *testing.T) {
	repo := &fakeProductRepository{}
	svc := NewCatalogService(repo, nil)

	// A client asking for 500 items per page shouldn't be able to force an unbounded query —
	// FR-CATALOG-6 / openapi.yaml's SizeParam caps this at 100.
	if _, _, err := svc.ListProducts(context.Background(), ListFilter{PageSize: 500}); err != nil {
		t.Fatalf("ListProducts: %v", err)
	}

	if repo.lastQuery.PageSize != defaultPageSize {
		t.Errorf("PageSize = %d, want it clamped back to the default %d", repo.lastQuery.PageSize, defaultPageSize)
	}
}

func TestListProductsPassesThroughValidInput(t *testing.T) {
	repo := &fakeProductRepository{}
	svc := NewCatalogService(repo, nil)

	categoryID := 3
	filter := ListFilter{Query: "bluetooth", CategoryID: &categoryID, Page: 2, PageSize: 10}
	if _, _, err := svc.ListProducts(context.Background(), filter); err != nil {
		t.Fatalf("ListProducts: %v", err)
	}

	if repo.lastQuery != filter {
		t.Errorf("repository received %+v, want it unchanged: %+v", repo.lastQuery, filter)
	}
}

func TestGetProductNotFound(t *testing.T) {
	wantErr := errors.New("simulated not found")
	repo := &fakeProductRepository{getIDErr: wantErr}
	svc := NewCatalogService(repo, nil)

	_, err := svc.GetProduct(context.Background(), 999)
	if !errors.Is(err, wantErr) {
		t.Errorf("GetProduct error = %v, want %v", err, wantErr)
	}
}
