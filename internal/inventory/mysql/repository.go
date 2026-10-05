package mysql

import (
	"context"
	"database/sql"
	"errors"

	driver "github.com/go-sql-driver/mysql"

	"brightbuy-backend/internal/inventory/app"
	"brightbuy-backend/internal/inventory/domain"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) Search(
	ctx context.Context,
	query string,
	page int,
	size int,
) ([]domain.VariantStock, int, error) {
	const whereClause = `
        FROM product_variant pv
        JOIN product p ON p.product_id = pv.product_id
        WHERE (
            ? = ''
            OR pv.sku = ?
            OR pv.sku LIKE CONCAT(?, '%')
            OR p.name LIKE CONCAT('%', ?, '%')
        )`

	args := []any{
		query,
		query,
		query,
		query,
	}

	var total int

	countQuery := `SELECT COUNT(*) ` + whereClause

	if err := r.db.QueryRowContext(
		ctx,
		countQuery,
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * size

	listQuery := `
        SELECT
            pv.variant_id,
            pv.sku,
            p.name,
            pv.stock_quantity
        ` + whereClause + `
        ORDER BY pv.variant_id
        LIMIT ? OFFSET ?`

	listArgs := append(args, size, offset)

	rows, err := r.db.QueryContext(
		ctx,
		listQuery,
		listArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]domain.VariantStock, 0)

	for rows.Next() {
		var item domain.VariantStock

		if err := rows.Scan(
			&item.VariantID,
			&item.SKU,
			&item.ProductName,
			&item.StockQuantity,
		); err != nil {
			return nil, 0, err
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

func (r *Repository) Adjust(
	ctx context.Context,
	actingUserID int,
	variantID int,
	delta int,
	reason string,
) error {
	_, err := r.db.ExecContext(
		ctx,
		`CALL sp_adjust_stock(?, ?, ?, ?)`,
		variantID,
		delta,
		reason,
		actingUserID,
	)
	if err == nil {
		return nil
	}

	var mysqlErr *driver.MySQLError

	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 4001:
			return app.ErrVariantNotFound
		case 4002:
			return app.ErrAdjustmentBelowZero
		}
	}

	return err
}

/*Searches variants by:
Exact SKU.
SKU prefix.
Product name.
Returns exact stock quantity for staff.
Uses SQL placeholders for all values.
Calls sp_adjust_stock.
Converts database error codes into application errors.
Prevents raw MySQL errors from reaching the HTTP client.*/
