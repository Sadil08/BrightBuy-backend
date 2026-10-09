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

// ListCities returns every city, alphabetically — the choices a customer sees at checkout.
func (r *Repository) ListCities(ctx context.Context) ([]domain.City, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT city_id, name FROM city ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cities := make([]domain.City, 0)
	for rows.Next() {
		var c domain.City
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		cities = append(cities, c)
	}
	return cities, rows.Err()
}
