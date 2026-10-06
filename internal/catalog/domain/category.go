package domain

// Category is a grouping a guest can browse the catalog by, or filter product search results with
// (FR-CATALOG-4). It mirrors the `category` table's public columns one-to-one — `is_active` is
// deliberately absent here, because by the time a Category reaches this type, the repository has
// already filtered out inactive rows (plan.md §6: an inactive category disappears from browse/filter
// immediately). There's simply nothing for a caller of this package to do with that flag, so it isn't
// carried any further than the query that used it.
type Category struct {
	ID          int
	Name        string
	Description string
}
