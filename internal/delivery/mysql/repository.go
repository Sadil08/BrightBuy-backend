package mysql

import (
	"context"
	"database/sql"

	"brightbuy-backend/internal/delivery/domain"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) EstimateDays(
	ctx context.Context,
	mode domain.DeliveryMode,
	cityID *int,
	itemsJSON []byte,
) (int, error) {
	var cityArg any
	if cityID != nil {
		cityArg = *cityID
	}

	var days int

	err := r.db.QueryRowContext(
		ctx,
		`SELECT fn_estimate_delivery_days(?, ?, ?)`,
		mode,
		cityArg,
		itemsJSON,
	).Scan(&days)
	if err != nil {
		return 0, err
	}

	return days, nil
}
