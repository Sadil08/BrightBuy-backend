// Package app is catalog's use-case layer: one service method per thing a caller actually wants to
// do (list products, get one product, list categories), each just a thin pass-through to whichever
// repository is wired in — see specs/global/06_ENGINEERING_STANDARDS.md §3.
package app

import (
	"context"
	"errors"

	"brightbuy-backend/internal/catalog/domain"
)

// ErrNotFound is the "no such thing" sentinel every ProductRepository implementation returns —
// declared HERE, at the port level, rather than inside mysql, specifically so httpapi can check for
// it with errors.Is without ever importing mysql directly (the same rule ListFilter's doc comment
// explains). "Not found" is a generic repository concept, not a MySQL-specific one — whatever
// concrete adapter is wired into CatalogService (mysql today, conceivably something else later)
// returns THIS value, wrapped with its own context via %w, never a locally-defined one of its own.
var ErrNotFound = errors.New("app: not found")

// ListFilter is the input to ListProducts: a search keyword, an optional category filter, and
// pagination (FR-CATALOG-3, FR-CATALOG-4, FR-CATALOG-6). It's declared here, in app, rather than in
// mysql, because httpapi needs to build one from query parameters and hand it to CatalogService —
// and httpapi is never allowed to import mysql directly (specs/global/06_ENGINEERING_STANDARDS.md
// §7). mysql.ProductRepository imports this package instead, to implement the interface below.
type ListFilter struct {
	Query      string
	CategoryID *int // nil means "no category filter" — a plain int can't represent "absent"
	Page       int  // 1-based
	PageSize   int
}

// ProductRepository is the PORT this service needs: the shape of "something that can fetch
// products," described without any mention of SQL, MySQL, or database/sql anywhere in it. The
// concrete implementation (the ADAPTER) is mysql.ProductRepository, wired in by cmd/api/main.go —
// this interface is what makes that swappable, and what lets CatalogService be unit-tested against
// a fake instead of a real database.
type ProductRepository interface {
	List(ctx context.Context, filter ListFilter) ([]domain.Product, int, error)
	GetByID(ctx context.Context, id int) (*domain.Product, error)
}

// CategoryRepository is a second, separate, narrow port (Interface Segregation —
// specs/global/06_ENGINEERING_STANDARDS.md §2) rather than one bloated Repository interface that
// mixes products and categories together.
type CategoryRepository interface {
	List(ctx context.Context) ([]domain.Category, error)
}

// CatalogService is the one type httpapi ever talks to for this feature — it never sees a
// repository directly.
type CatalogService struct {
	products   ProductRepository
	categories CategoryRepository
}

// NewCatalogService is the only way to build a CatalogService, and the only place that ever will be
// called is cmd/api/main.go (the composition root) — everywhere else just receives an already-built
// *CatalogService.
func NewCatalogService(products ProductRepository, categories CategoryRepository) *CatalogService {
	return &CatalogService{products: products, categories: categories}
}

// defaultPageSize/maxPageSize match specs/openapi/openapi.yaml's SizeParam (default 20, max 100) —
// FR-CATALOG-6. Clamping bad input to something sane is business policy, so it lives here in app,
// not scattered into httpapi (which just parses request query params) or mysql (which should be
// able to trust the filter it's handed).
const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// Normalized returns a copy of f with Page/PageSize defaulted and clamped to sane bounds
// (FR-CATALOG-6; matches openapi.yaml's PageParam/SizeParam: page >= 1, 1 <= size <= 100).
//
// This is its own method, called by BOTH ListProducts below AND httpapi's handler, rather than
// logic inlined only here, for a specific reason: the handler needs to report the page/size that was
// ACTUALLY used back in the response's pagination envelope (e.g. a request with no ?page= at all
// should report back "page": 1, not "page": 0). ListFilter is a plain struct, passed BY VALUE — if
// only ListProducts clamped its own local copy, the handler's copy would never see that change, and
// would report the wrong numbers. Calling the exact same Normalized() method in both places means
// there's only one piece of logic that can ever drift, instead of two copies quietly disagreeing.
func (f ListFilter) Normalized() ListFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > maxPageSize {
		f.PageSize = defaultPageSize
	}
	return f
}

// ListProducts is the browse/search/filter use case (FR-CATALOG-1,3,4,6). It doesn't run any SQL
// itself — it normalizes the input, then delegates to whichever ProductRepository was injected.
func (s *CatalogService) ListProducts(ctx context.Context, filter ListFilter) ([]domain.Product, int, error) {
	filter = filter.Normalized()
	return s.products.List(ctx, filter)
}

// GetProduct is the product-detail use case (FR-CATALOG-2,5,7). A nil, nil result never happens —
// the repository returns app.ErrNotFound (wrapped) when nothing matches, which httpapi translates
// into a 404.
func (s *CatalogService) GetProduct(ctx context.Context, id int) (*domain.Product, error) {
	return s.products.GetByID(ctx, id)
}

// ListCategories is the category-browse use case (FR-CATALOG-4's "what can I filter by").
func (s *CatalogService) ListCategories(ctx context.Context) ([]domain.Category, error) {
	return s.categories.List(ctx)
}
