package domain

import "time"

// Product is the top-level entity a guest browses, searches, and views the detail of
// (FR-CATALOG-1..3). Categories and Variants are slices — populated by the mysql layer running
// separate queries against product_category and product_variant and assembling the results here —
// this package has no idea a database, a join, or SQL exists; it just describes what a Product
// looks like once assembled. time.Time is standard library, so it doesn't break domain's
// "imports nothing but stdlib" rule the way money.Money's exception (variant.go) does.
type Product struct {
	ID          int
	Name        string
	Description string
	Categories  []Category
	Variants    []Variant
	Images      []ProductImage // oldest first; the first is the primary image
	// UpdatedAt is the most recent change to EITHER this product's own row OR any of its variants'
	// rows (whichever is later) — see mysql.ProductRepository.GetByID. It exists for exactly one
	// reason: httpapi derives a weak ETag from it (plan.md §1) for the product-detail endpoint. It's
	// deliberately not populated by List (plan.md's ETag plan is "a row," singular — a page of 20
	// products has no single natural "last modified" moment to key a collection ETag off of), so a
	// Product returned from List always has UpdatedAt at its zero value.
	UpdatedAt time.Time
}

// ProductImage is a stored image's public face; the URL is derived from ObjectKey at the HTTP edge,
// so the domain never learns where the object store lives.
type ProductImage struct {
	ID        int64
	ObjectKey string
}
