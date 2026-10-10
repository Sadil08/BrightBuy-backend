package mysql

import (
	"context"
	"database/sql"
	"fmt"
)

type PaymentRepository struct {
}

func NewPaymentRepository() *PaymentRepository {
	return &PaymentRepository{}
}

func (r *PaymentRepository) SetStatus(ctx context.Context, tx *sql.Tx, orderID int, status string) error {
	_, err := tx.ExecContext(ctx, "UPDATE payment SET status = ? WHERE order_id = ?", status, orderID)
	if err != nil {
		return fmt.Errorf("set payment status for order %d to %s: %w", orderID, status, err)
	}
	return nil
}
