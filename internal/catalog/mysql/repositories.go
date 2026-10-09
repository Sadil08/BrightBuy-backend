package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/dbx"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateProduct(ctx context.Context, product domain.Product, categoryIDs []int64, variants []app.VariantInput) (created domain.Product, err error) {
	err = dbx.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO product (name, description, is_active) VALUES (?, ?, ?)`, product.Name, nullableString(product.Description), product.Active)
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		created = product
		created.ID = id
		for _, categoryID := range categoryIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO product_category (product_id, category_id) VALUES (?, ?)`, id, categoryID); err != nil {
				return err
			}
		}
		for _, variant := range variants {
			if _, err := tx.ExecContext(ctx, `INSERT INTO product_variant (product_id, sku, price, stock_quantity, is_active) VALUES (?, ?, ?, ?, TRUE)`, id, variant.SKU, centsToDecimal(variant.PriceCents), variant.StockQuantity); err != nil {
				return mapDBError(err)
			}
		}
		return nil
	})
	return created, err
}

func (r *Repository) UpdateProduct(ctx context.Context, id int64, patch app.ProductPatch) error {
	sets, args := []string{}, []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*patch.Name))
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, nullableString(*patch.Description))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	result, err := r.db.ExecContext(ctx, `UPDATE product SET `+strings.Join(sets, ", ")+` WHERE product_id = ?`, args...)
	if err != nil {
		return mapDBError(err)
	}
	return changedOrNotFound(result)
}

func (r *Repository) SetProductActive(ctx context.Context, id int64, active bool) error {
	result, err := r.db.ExecContext(ctx, `UPDATE product SET is_active = ? WHERE product_id = ?`, active, id)
	if err != nil {
		return err
	}
	return changedOrNotFound(result)
}

func (r *Repository) CreateVariant(ctx context.Context, productID int64, input app.VariantInput) (variant domain.Variant, err error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO product_variant (product_id, sku, price, stock_quantity, is_active) VALUES (?, ?, ?, ?, TRUE)`, productID, input.SKU, centsToDecimal(input.PriceCents), input.StockQuantity)
	if err != nil {
		return domain.Variant{}, mapDBError(err)
	}
	variant = domain.Variant{ProductID: productID, SKU: input.SKU, PriceCents: input.PriceCents, StockQuantity: input.StockQuantity, Active: true}
	variant.ID, err = result.LastInsertId()
	return variant, err
}

func (r *Repository) UpdateVariant(ctx context.Context, id int64, patch app.VariantPatch) error {
	sets, args := []string{}, []any{}
	if patch.SKU != nil {
		sets = append(sets, "sku = ?")
		args = append(args, strings.TrimSpace(*patch.SKU))
	}
	if patch.PriceCents != nil {
		sets = append(sets, "price = ?")
		args = append(args, centsToDecimal(*patch.PriceCents))
	}
	if patch.StockQuantity != nil {
		sets = append(sets, "stock_quantity = ?")
		args = append(args, *patch.StockQuantity)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	result, err := r.db.ExecContext(ctx, `UPDATE product_variant SET `+strings.Join(sets, ", ")+` WHERE variant_id = ?`, args...)
	if err != nil {
		return mapDBError(err)
	}
	return changedOrNotFound(result)
}

func (r *Repository) SetVariantActive(ctx context.Context, id int64, active bool) error {
	result, err := r.db.ExecContext(ctx, `UPDATE product_variant SET is_active = ? WHERE variant_id = ?`, active, id)
	if err != nil {
		return err
	}
	return changedOrNotFound(result)
}

func (r *Repository) CreateCategory(ctx context.Context, input app.CategoryInput) (category domain.Category, err error) {
	result, err := r.db.ExecContext(ctx, `INSERT INTO category (name, description, is_active) VALUES (?, ?, TRUE)`, strings.TrimSpace(input.Name), nullableString(input.Description))
	if err != nil {
		return domain.Category{}, mapDBError(err)
	}
	category = domain.Category{Name: strings.TrimSpace(input.Name), Description: input.Description, Active: true}
	category.ID, err = result.LastInsertId()
	return category, err
}

func (r *Repository) UpdateCategory(ctx context.Context, id int64, patch app.CategoryPatch) error {
	sets, args := []string{}, []any{}
	if patch.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, strings.TrimSpace(*patch.Name))
	}
	if patch.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, nullableString(*patch.Description))
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	result, err := r.db.ExecContext(ctx, `UPDATE category SET `+strings.Join(sets, ", ")+` WHERE category_id = ?`, args...)
	if err != nil {
		return mapDBError(err)
	}
	return changedOrNotFound(result)
}

func (r *Repository) SetCategoryActive(ctx context.Context, id int64, active bool) error {
	result, err := r.db.ExecContext(ctx, `UPDATE category SET is_active = ? WHERE category_id = ?`, active, id)
	if err != nil {
		return err
	}
	return changedOrNotFound(result)
}

func (r *Repository) ActiveCategoriesExist(ctx context.Context, ids []int64) (bool, error) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i], args[i] = "?", id
	}
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM category WHERE is_active = TRUE AND category_id IN (`+strings.Join(placeholders, ",")+")", args...).Scan(&count)
	return count == len(ids), err
}

func mapDBError(err error) error {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 && strings.Contains(mysqlErr.Message, "uq_product_variant_sku") {
		return domain.ErrSKUConflict
	}
	return err
}

func changedOrNotFound(result sql.Result) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func centsToDecimal(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

var _ app.Repository = (*Repository)(nil)
