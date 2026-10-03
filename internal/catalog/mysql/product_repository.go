package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
)

// ProductRepository is the ADAPTER for app.ProductRepository. It imports app for exactly one
// reason: ListFilter, the parameter type app's port interface declares — this is the "mysql knows
// about app only to implement app's interfaces" rule from learn-go/11.
type ProductRepository struct {
	db *sql.DB
}

func NewProductRepository(db *sql.DB) *ProductRepository {
	return &ProductRepository{db: db}
}

// List implements the browse/search/filter/paginate query (plan.md §5.1). It always runs exactly
// three queries, regardless of how many products are on the page: one to count total matches (for
// pagination), one for the page of products itself, and one BATCHED query that fetches every
// variant for every product on the page at once — see attachVariants below for why that matters.
func (r *ProductRepository) List(ctx context.Context, filter app.ListFilter) ([]domain.Product, int, error) {
	var total int
	countWhere, countArgs := buildProductWhere(filter)
	countQuery := "SELECT COUNT(*) FROM product p " + countWhere
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("mysql: count products: %w", err)
	}

	// Built fresh rather than reusing countArgs: LIMIT/OFFSET get appended below, and building a
	// second, independent slice avoids any risk of the two queries' arguments aliasing each other.
	where, args := buildProductWhere(filter)
	offset := (filter.Page - 1) * filter.PageSize
	query := "SELECT p.product_id, p.name, COALESCE(p.description, '') FROM product p " + where +
		" ORDER BY p.product_id LIMIT ? OFFSET ?"
	args = append(args, filter.PageSize, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("mysql: list products: %w", err)
	}
	defer rows.Close()

	var products []domain.Product
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Description); err != nil {
			return nil, 0, fmt.Errorf("mysql: scan product: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("mysql: list products: rows: %w", err)
	}

	if err := r.attachVariants(ctx, products); err != nil {
		return nil, 0, err
	}

	return products, total, nil
}

// buildProductWhere turns a filter into a WHERE clause and its matching argument list — built as
// one function so the count query and the page query can never accidentally drift apart (plan.md
// §5.1). Every value is still a `?` placeholder; only the CLAUSE STRUCTURE (whether a condition is
// present at all) depends on the filter — never a value is concatenated into the SQL text
// (specs/global/07_SQL_DATABASE_STANDARDS.md §2, §4: no exceptions).
//
// Worth noticing what ISN'T here: no special-casing for "search matched nothing" or "unknown
// categoryId." Both just fall out of ordinary SQL — an AND EXISTS(...) that matches no category ID
// filters everything out, a MATCH...AGAINST with no hits returns zero rows — so plan.md §6's "empty
// results are a 200, not an error" edge case needs no extra code at all here; it's just what SQL
// already does.
func buildProductWhere(filter app.ListFilter) (string, []any) {
	where := "WHERE p.is_active = TRUE"
	var args []any

	if filter.Query != "" {
		where += " AND MATCH(p.name, p.description) AGAINST (? IN NATURAL LANGUAGE MODE)" // FR-CATALOG-3
		args = append(args, filter.Query)
	}
	if filter.CategoryID != nil {
		where += " AND EXISTS (SELECT 1 FROM product_category pc WHERE pc.product_id = p.product_id AND pc.category_id = ?)" // FR-CATALOG-4
		args = append(args, *filter.CategoryID)
	}

	return where, args
}

// attachVariants fills in products[i].Variants for every product in the slice using ONE query, not
// one query per product. That matters: a naive "for each product, SELECT its variants" loop is the
// classic N+1 query bug — fine for 1 product, ruinous for a page of 20 (20 extra round-trips to the
// database instead of 1). The `IN (?,?,?...)` placeholder list is built dynamically here because SQL
// has no syntax for "however many values happen to be in this slice" — but only the CLAUSE gets
// built with fmt.Sprintf/strings.Join, never a value: every actual product ID still travels as its
// own `?` argument in ids, exactly like any other parameterized query.
//
// The list endpoint (ProductSummary in openapi.yaml) only needs each variant's price and stock
// status to derive priceFrom/stockStatus — not its attributes — so this intentionally skips the
// attribute join that GetProduct's selectVariantsForProduct does. Keeping those two variant-fetch
// paths separate (rather than one "does everything" function) is what keeps this one cheap for a
// list page instead of paying for detail-page data nothing here will use.
func (r *ProductRepository) attachVariants(ctx context.Context, products []domain.Product) error {
	if len(products) == 0 {
		return nil
	}

	ids := make([]any, len(products))
	indexByProductID := make(map[int]int, len(products))
	placeholders := make([]string, len(products))
	for i, p := range products {
		ids[i] = p.ID
		indexByProductID[p.ID] = i
		placeholders[i] = "?"
	}

	query := fmt.Sprintf(
		`SELECT product_id, variant_id, sku, price, fn_is_variant_in_stock(variant_id)
		 FROM product_variant
		 WHERE product_id IN (%s) AND is_active = TRUE
		 ORDER BY product_id, variant_id`,
		strings.Join(placeholders, ","),
	)

	rows, err := r.db.QueryContext(ctx, query, ids...)
	if err != nil {
		return fmt.Errorf("mysql: list variants for product page: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			productID int
			v         domain.Variant
			inStock   bool
		)
		if err := rows.Scan(&productID, &v.ID, &v.SKU, &v.Price, &inStock); err != nil {
			return fmt.Errorf("mysql: scan variant: %w", err)
		}
		v.Stock = stockStatusFrom(inStock)

		idx := indexByProductID[productID]
		products[idx].Variants = append(products[idx].Variants, v)
	}
	return rows.Err()
}

// GetByID implements the product-detail query (FR-CATALOG-2,5,7): the product row, its active
// categories, and its active variants with full attributes and derived stock status. Four small,
// separate queries rather than one giant JOIN — a product-category-variant-attribute join would
// multiply rows (one row per variant per attribute per category), which then has to be de-duplicated
// back apart in Go anyway. Separate queries, assembled here, read more directly.
func (r *ProductRepository) GetByID(ctx context.Context, id int) (*domain.Product, error) {
	product, err := r.selectProduct(ctx, id)
	if err != nil {
		return nil, err
	}

	categories, err := r.selectCategoriesForProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	product.Categories = categories

	variants, variantsMaxUpdatedAt, err := r.selectVariantsForProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	product.Variants = variants

	// product.UpdatedAt starts as the product row's own timestamp (set in selectProduct below); a
	// variant changing price/stock more recently than the product row itself was last touched
	// should ALSO count as "this detail view changed" for ETag purposes (a stock_quantity update
	// doesn't touch the product table at all, only product_variant).
	if variantsMaxUpdatedAt.After(product.UpdatedAt) {
		product.UpdatedAt = variantsMaxUpdatedAt
	}

	return product, nil
}

func (r *ProductRepository) selectProduct(ctx context.Context, id int) (*domain.Product, error) {
	var p domain.Product
	err := r.db.QueryRowContext(ctx,
		`SELECT product_id, name, COALESCE(description, ''), updated_at FROM product WHERE product_id = ? AND is_active = TRUE`,
		id,
	).Scan(&p.ID, &p.Name, &p.Description, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// A deactivated OR nonexistent product both land here identically — plan.md §6: "gone
			// means gone," no distinction, both become a 404 in httpapi.
			return nil, fmt.Errorf("mysql: product %d: %w", id, app.ErrNotFound)
		}
		return nil, fmt.Errorf("mysql: get product %d: %w", id, err)
	}
	return &p, nil
}

func (r *ProductRepository) selectCategoriesForProduct(ctx context.Context, productID int) ([]domain.Category, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT c.category_id, c.name, COALESCE(c.description, '')
		 FROM category c
		 JOIN product_category pc ON pc.category_id = c.category_id
		 WHERE pc.product_id = ? AND c.is_active = TRUE
		 ORDER BY c.name`,
		productID,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql: list categories for product %d: %w", productID, err)
	}
	defer rows.Close()

	var categories []domain.Category
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, fmt.Errorf("mysql: scan category: %w", err)
		}
		categories = append(categories, c)
	}
	return categories, rows.Err()
}

// selectVariantsForProduct fetches one product's variants AND their attributes, in two queries
// total (never one query per variant): the second query joins across all three EAV tables keyed by
// product_id directly, so it returns every attribute of every variant of this one product in a
// single round-trip, however many variants there are.
// selectVariantsForProduct returns the product's variants AND the most recent updated_at across all
// of them (time.Time{}, the zero value, if there are none) — the second return exists purely so
// GetByID can fold it into product.UpdatedAt for the ETag; nowhere does a per-variant timestamp get
// exposed in domain.Variant itself, since nothing outside this one call site needs it.
func (r *ProductRepository) selectVariantsForProduct(ctx context.Context, productID int) ([]domain.Variant, time.Time, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT variant_id, sku, price, fn_is_variant_in_stock(variant_id), updated_at
		 FROM product_variant
		 WHERE product_id = ? AND is_active = TRUE
		 ORDER BY variant_id`,
		productID,
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("mysql: list variants for product %d: %w", productID, err)
	}
	defer rows.Close()

	var variants []domain.Variant
	var maxUpdatedAt time.Time
	for rows.Next() {
		var (
			v         domain.Variant
			inStock   bool
			updatedAt time.Time
		)
		if err := rows.Scan(&v.ID, &v.SKU, &v.Price, &inStock, &updatedAt); err != nil {
			return nil, time.Time{}, fmt.Errorf("mysql: scan variant: %w", err)
		}
		v.Stock = stockStatusFrom(inStock)
		v.Attributes = map[string]string{}
		variants = append(variants, v)
		if updatedAt.After(maxUpdatedAt) {
			maxUpdatedAt = updatedAt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("mysql: list variants for product %d: rows: %w", productID, err)
	}
	if len(variants) == 0 {
		return variants, maxUpdatedAt, nil
	}

	// variantByID holds pointers INTO the variants slice, so the loop below can fill in each
	// variant's Attributes map in place. This is only safe because we're done appending to
	// variants by this point — append can reallocate the underlying array, which would silently
	// leave these pointers referencing stale, discarded memory instead of the slice's real elements.
	variantByID := make(map[int]*domain.Variant, len(variants))
	for i := range variants {
		variantByID[variants[i].ID] = &variants[i]
	}

	attrRows, err := r.db.QueryContext(ctx,
		`SELECT va.variant_id, an.name, av.value
		 FROM variant_attribute va
		 JOIN attribute_value av ON av.attribute_value_id = va.attribute_value_id
		 JOIN attribute_name an ON an.attribute_name_id = av.attribute_name_id
		 JOIN product_variant pv ON pv.variant_id = va.variant_id
		 WHERE pv.product_id = ?
		 ORDER BY va.variant_id, an.name`,
		productID,
	)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("mysql: list variant attributes for product %d: %w", productID, err)
	}
	defer attrRows.Close()

	for attrRows.Next() {
		var variantID int
		var name, value string
		if err := attrRows.Scan(&variantID, &name, &value); err != nil {
			return nil, time.Time{}, fmt.Errorf("mysql: scan variant attribute: %w", err)
		}
		if v, ok := variantByID[variantID]; ok {
			v.Attributes[name] = value
		}
	}
	if err := attrRows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("mysql: list variant attributes for product %d: rows: %w", productID, err)
	}

	return variants, maxUpdatedAt, nil
}

func stockStatusFrom(inStock bool) domain.StockStatus {
	if inStock {
		return domain.InStock
	}
	return domain.OutOfStock
}
