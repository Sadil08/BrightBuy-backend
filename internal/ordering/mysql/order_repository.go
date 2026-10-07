package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"brightbuy-backend/internal/cart/domain"
	"brightbuy-backend/internal/ordering/app"
	orderdomain "brightbuy-backend/internal/ordering/domain"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type OrderRepository struct {
	db *sql.DB
}

const quotedOrderTable = "`order`"

func NewOrderRepository(db *sql.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

func (r *OrderRepository) GetConfig(ctx context.Context, key string) (string, error) {
	var value string
	err := r.db.QueryRowContext(ctx,
		`SELECT config_value FROM app_config WHERE config_key = ?`,
		key,
	).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("read app_config %q: %w", key, err)
	}
	return value, nil
}

func (r *OrderRepository) CityExists(ctx context.Context, cityID int) (bool, error) {
	var exists bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM city WHERE city_id = ?)`, cityID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check delivery city: %w", err)
	}
	return exists, nil
}

func (r *OrderRepository) FindByIdempotencyKey(ctx context.Context, customerID int, key string) (int, error) {
	var orderID int
	err := r.db.QueryRowContext(ctx,
		`SELECT order_id FROM `+quotedOrderTable+` WHERE customer_id = ? AND idempotency_key = ?`,
		customerID, key,
	).Scan(&orderID)
	if err != nil {
		return 0, err
	}
	return orderID, nil
}

func (r *OrderRepository) CallPlaceOrder(ctx context.Context, tx *sql.Tx, command app.PlaceOrderCommand) (int, bool, error) {
	var cityID any
	if command.DeliveryCityID != nil {
		cityID = *command.DeliveryCityID
	}

	_, err := tx.ExecContext(ctx, `
		CALL sp_place_order(
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, @checkout_order_id, @checkout_created
		)
	`,
		command.CustomerID,
		string(command.ItemsJSON),
		command.DeliveryMode,
		cityID,
		nullableText(command.DeliveryAddress),
		command.PaymentMethod,
		command.IdempotencyKey,
		centsAsDecimal(command.TaxAmountCents),
		centsAsDecimal(command.DeliveryFeeCents),
		nullableText(command.ProviderReference),
		nullableText(command.CardLastFour),
	)
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Message == "STOCK_EXCEEDED" {
			return 0, false, app.ErrStockExceeded
		}
		return 0, false, fmt.Errorf("call sp_place_order: %w", err)
	}

	var orderID int
	var created bool
	if err := tx.QueryRowContext(ctx, `SELECT @checkout_order_id, @checkout_created`).Scan(&orderID, &created); err != nil {
		return 0, false, fmt.Errorf("read sp_place_order result: %w", err)
	}
	if orderID <= 0 {
		return 0, false, fmt.Errorf("sp_place_order returned invalid order id %d", orderID)
	}
	return orderID, created, nil
}

func (r *OrderRepository) GetByID(ctx context.Context, customerID, orderID int) (*orderdomain.Order, error) {
	var order orderdomain.Order
	var subtotal, tax, fee, total string
	var mode string
	var estimatedDate time.Time
	err := r.db.QueryRowContext(ctx, `
		SELECT o.order_id, o.status, o.subtotal, o.tax_amount, o.delivery_fee, o.total_amount,
		       o.order_date, d.mode, d.estimated_days, d.estimated_date, p.status
		FROM `+quotedOrderTable+` o
		JOIN delivery d ON d.order_id = o.order_id
		JOIN payment p ON p.order_id = o.order_id
		WHERE o.order_id = ? AND o.customer_id = ?
	`, orderID, customerID).Scan(
		&order.ID, &order.Status, &subtotal, &tax, &fee, &total,
		&order.CreatedAt, &mode, &order.Delivery.EstimatedDays, &estimatedDate, &order.PaymentStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, app.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get order %d: %w", orderID, err)
	}
	order.Subtotal = subtotal
	order.TaxAmount = tax
	order.DeliveryFee = fee
	order.TotalAmount = total
	order.Delivery.Mode = orderdomain.DeliveryMode(mode)
	order.Delivery.EstimatedDate = estimatedDate.Format("2006-01-02")
	order.Items = make([]orderdomain.OrderItem, 0)

	rows, err := r.db.QueryContext(ctx, `
		SELECT oi.variant_id, COALESCE(p.name, ''), oi.quantity, oi.unit_price_at_order
		FROM order_item oi
		JOIN product_variant pv ON pv.variant_id = oi.variant_id
		JOIN product p ON p.product_id = pv.product_id
		WHERE oi.order_id = ?
		ORDER BY oi.order_item_id
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("list order %d items: %w", orderID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var item orderdomain.OrderItem
		if err := rows.Scan(&item.VariantID, &item.ProductName, &item.Quantity, &item.UnitPriceAtOrder); err != nil {
			return nil, fmt.Errorf("scan order item: %w", err)
		}
		order.Items = append(order.Items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read order %d items: %w", orderID, err)
	}
	return &order, nil
}

func (r *OrderRepository) ListByCustomer(ctx context.Context, customerID, page, size int) ([]orderdomain.Order, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+quotedOrderTable+` WHERE customer_id = ?`,
		customerID,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customer orders: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT order_id FROM `+quotedOrderTable+`
		WHERE customer_id = ?
		ORDER BY order_date DESC, order_id DESC
		LIMIT ? OFFSET ?
	`, customerID, size, (page-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list customer orders: %w", err)
	}
	ids := make([]int, 0, size)
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan order id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("read customer order ids: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, fmt.Errorf("close customer order rows: %w", err)
	}

	orders := make([]orderdomain.Order, 0, len(ids))
	for _, id := range ids {
		order, err := r.GetByID(ctx, customerID, id)
		if err != nil {
			return nil, 0, err
		}
		orders = append(orders, *order)
	}
	return orders, total, nil
}

func (r *OrderRepository) DiagnoseUnavailableLines(ctx context.Context, items []domain.CartItem) ([]orderdomain.UnavailableLine, error) {
	unavailable := make([]orderdomain.UnavailableLine, 0)
	for _, item := range items {
		var name string
		var stock sql.NullInt64
		var available bool
		err := r.db.QueryRowContext(ctx, `
			SELECT COALESCE(p.name, ''), pv.stock_quantity,
			       (pv.is_active = TRUE AND p.is_active = TRUE)
			FROM product_variant pv
			JOIN product p ON p.product_id = pv.product_id
			WHERE pv.variant_id = ?
		`, item.VariantID).Scan(&name, &stock, &available)
		if errors.Is(err, sql.ErrNoRows) {
			unavailable = append(unavailable, orderdomain.UnavailableLine{
				VariantID: item.VariantID, ProductName: item.ProductName,
				Requested: item.Quantity, Available: 0,
			})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("diagnose variant %d availability: %w", item.VariantID, err)
		}
		availableQty := 0
		if stock.Valid {
			availableQty = int(stock.Int64)
		}
		if !available || availableQty < item.Quantity {
			unavailable = append(unavailable, orderdomain.UnavailableLine{
				VariantID: item.VariantID, ProductName: name,
				Requested: item.Quantity, Available: availableQty,
			})
		}
	}
	return unavailable, nil
}

func nullableText(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func centsAsDecimal(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
