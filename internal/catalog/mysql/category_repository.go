package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"brightbuy-backend/internal/catalog/domain"
)

// CategoryRepository is the ADAPTER implementing app.CategoryRepository — it's never referred to by
// that interface name here, because it doesn't need to be: Go checks the method set matches
// structurally at the point it's used as one (cmd/api/main.go), not here.
type CategoryRepository struct {
	db *sql.DB
}

func NewCategoryRepository(db *sql.DB) *CategoryRepository {
	return &CategoryRepository{db: db}
}

// List returns every active category (FR-CATALOG-4). Deactivated categories (is_active = FALSE,
// set by 07-admin-catalog) are excluded here at the query level — plan.md §6: a category
// deactivated by staff disappears from the storefront's filter list immediately.
func (r *CategoryRepository) List(ctx context.Context) ([]domain.Category, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT category_id, name, COALESCE(description, '') FROM category WHERE is_active = TRUE ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("mysql: list categories: %w", err)
	}
	defer rows.Close() // always closes rows even if the loop below returns early on an error

	var categories []domain.Category
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Description); err != nil {
			return nil, fmt.Errorf("mysql: scan category: %w", err)
		}
		categories = append(categories, c)
	}
	// rows.Err() catches a failure that happened DURING iteration (e.g. the connection dropped
	// mid-stream) — Scan itself only reports per-row problems, so skipping this check is a classic
	// way to silently return a truncated result set instead of an error.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("mysql: list categories: rows: %w", err)
	}

	return categories, nil
}
