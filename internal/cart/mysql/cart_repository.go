package mysql

// the database layer for the shopping cart in backend.

import (
	"context"
	"database/sql"
	"fmt" // for error formatting
	"strconv"
	"strings"

	"brightbuy-backend/internal/cart/domain"
)

type CartRepository struct {
	db *sql.DB //an object containing a database connection
}

func NewCartRepository(db *sql.DB) *CartRepository {
	return &CartRepository{db: db}
}

/*
1. Check customerID is valid
2. Make sure customer has a cart
3. Find the cart ID
4. Get all cart items
5. Get product + variant information
6. Check whether items are available
7. Calculate line totals
8. Check stock
9. Calculate subtotal
10. Return Cart object
*/

// r represents the particular CartRepository object that called the method.
func (r *CartRepository) Get(
	ctx context.Context,
	customerID int,
) (*domain.Cart, error) {
	if customerID <= 0 {
		return nil, fmt.Errorf("customer ID must be positive") // Step 1: Check customerID is valid
	}

	// Ensure this customer has a cart; the unique key handles concurrent requests.
	// If the cart already exists, we just get its ID via LAST_INSERT_ID().
	_, err := r.db.ExecContext(ctx, `   
        INSERT INTO cart (customer_id)     
        VALUES (?)
        ON DUPLICATE KEY UPDATE cart_id = LAST_INSERT_ID(cart_id) 
    `, customerID)
	if err != nil {
		return nil, fmt.Errorf("ensure cart: %w", err) // Step 2: Make sure customer has a cart
	}

	cart := &domain.Cart{
		CustomerID: customerID,
		Items:      make([]domain.CartItem, 0), // Step 3: Find the cart ID
	}

	err = r.db.QueryRowContext(ctx, // Step 4: Get all cart items
		`SELECT cart_id FROM cart WHERE customer_id = ?`,
		customerID,
	).Scan(&cart.ID)
	if err != nil {
		return nil, fmt.Errorf("find cart: %w", err)
	}

	// Step 5: Get product + variant information, Step 6: Check whether items are available, Step 7: Calculate line totals, Step 8: Check stock, Step 9: Calculate subtotal
	rows, err := r.db.QueryContext(ctx, `
        SELECT        
            ci.cart_item_id,
            ci.variant_id,
            COALESCE(p.name, ''),
            pv.price,
            ci.quantity,
            pv.stock_quantity,
            CASE
                WHEN pv.variant_id IS NULL
                  OR pv.is_active = FALSE
                  OR p.is_active = FALSE
                THEN 1
                ELSE 0
            END
        FROM cart c
        JOIN cart_item ci ON ci.cart_id = c.cart_id
        LEFT JOIN product_variant pv ON pv.variant_id = ci.variant_id
        LEFT JOIN product p ON p.product_id = pv.product_id
        WHERE c.customer_id = ?
        ORDER BY ci.cart_item_id
    `, customerID)
	if err != nil {
		return nil, fmt.Errorf("query cart items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.CartItem
		var price sql.NullString
		var stock sql.NullInt64
		var unavailable int

		if err := rows.Scan( // Step 5: Get product + variant information
			&item.ID,
			&item.VariantID,
			&item.ProductName,
			&price,
			&item.Quantity,
			&stock,
			&unavailable,
		); err != nil {
			return nil, fmt.Errorf("scan cart item: %w", err)
		}

		item.Unavailable = unavailable == 1

		if price.Valid {
			item.UnitPrice, err = decimalToMoney(price.String)
			if err != nil {
				return nil, fmt.Errorf("parse cart item price: %w", err)
			}
		}

		item.LineTotal = item.UnitPrice * domain.Money(item.Quantity)
		availableStock := int64(0)
		if stock.Valid {
			availableStock = stock.Int64
		}
		item.StockWarning = !item.Unavailable && int64(item.Quantity) > availableStock

		// Step 9: Calculate subtotal
		if !item.Unavailable {
			cart.Subtotal += item.LineTotal
		}
		cart.Items = append(cart.Items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read cart items: %w", err)
	}

	return cart, nil
}

func (r *CartRepository) UpsertLine(
	ctx context.Context,
	customerID int,
	variantID int,
	quantity int,
) (*domain.Cart, error) {
	if customerID <= 0 || variantID <= 0 || quantity < 1 {
		return nil, fmt.Errorf("invalid input: customerID=%d, variantID=%d, quantity=%d", customerID, variantID, quantity)
	}
	_, err := r.db.ExecContext(ctx, `
	INSERT INTO cart (customer_id)
	VALUES (?)
	ON DUPLICATE KEY UPDATE cart_id = LAST_INSERT_ID(cart_id)
`, customerID)

	// Check whether the SQL query produced an error.

	if err != nil {
		return nil, fmt.Errorf("ensure cart: %w", err)
	}

	var cartID int

	// Find the cart belonging to this customer.

	err = r.db.QueryRowContext(ctx, `
	SELECT cart_id FROM cart WHERE customer_id = ?
`, customerID).Scan(&cartID)

	if err != nil {
		return nil, fmt.Errorf("find cart: %w", err)
	}

	// Add the product variant to the cart.
	_, err = r.db.ExecContext(ctx, `
    INSERT INTO cart_item (cart_id, variant_id, quantity)
    VALUES (?, ?, ?)

    -- If this cart already contains this variant,
    -- update its quantity instead of inserting another row.
    ON DUPLICATE KEY UPDATE quantity = VALUES(quantity)
    `, cartID, variantID, quantity)

	if err != nil {
		return nil, fmt.Errorf("upsert cart item: %w", err)
	}

	return r.Get(ctx, customerID)

}

// FindLine searches for a specific product variant
// in a customer's cart. It returns the CartItem if found, or nil if not found.
func (r *CartRepository) FindLine(
	ctx context.Context,
	customerID, variantID int,
) (*domain.CartItem, error) {
	//validate input parameters
	if customerID <= 0 || variantID <= 0 {
		return nil, fmt.Errorf("customer ID and variant ID must be positive")
	}

	var item domain.CartItem
	err := r.db.QueryRowContext(ctx, `
        SELECT ci.cart_item_id, ci.variant_id, ci.quantity
        FROM cart c
        JOIN cart_item ci ON ci.cart_id = c.cart_id
        WHERE c.customer_id = ? AND ci.variant_id = ?
    `, customerID, variantID).Scan(
		&item.ID, //Scan() takes  database values and puts them into your Go variables.
		&item.VariantID,
		&item.Quantity,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find cart line: %w", err)
	}

	return &item, nil
}

// DeleteLine removes a specific product variant from a customer's cart.
func (r *CartRepository) DeleteLine(
	ctx context.Context,
	customerID, variantID int,
) error {
	if customerID <= 0 || variantID <= 0 {
		return fmt.Errorf("customer ID and variant ID must be positive")
	}

	result, err := r.db.ExecContext(ctx, `
        DELETE ci
        FROM cart_item ci
        JOIN cart c ON c.cart_id = ci.cart_id
        WHERE c.customer_id = ? AND ci.variant_id = ?
    `, customerID, variantID)
	if err != nil {
		return fmt.Errorf("delete cart line: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil
	}

	return nil
}

func (r *CartRepository) ClearItemsInTx(ctx context.Context, tx *sql.Tx, customerID int) error {
	if tx == nil || customerID <= 0 {
		return fmt.Errorf("invalid cart clear transaction or customer ID")
	}
	_, err := tx.ExecContext(ctx, `
		DELETE ci
		FROM cart_item ci
		JOIN cart c ON c.cart_id = ci.cart_id
		WHERE c.customer_id = ?
	`, customerID)
	if err != nil {
		return fmt.Errorf("clear cart items: %w", err)
	}
	return nil
}

func decimalToMoney(value string) (domain.Money, error) {
	whole, fraction, hasFraction := strings.Cut(value, ".")

	if !hasFraction {
		fraction = ""
	}

	if len(fraction) > 2 {
		return 0, fmt.Errorf("expected a two-decimal price, got %q", value)
	}
	fraction += strings.Repeat("0", 2-len(fraction))

	dollars, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid dollar amount %q: %w", value, err)
	}
	cents, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cents in %q: %w", value, err)
	}

	return domain.Money(dollars*100 + cents), nil
}

// VariantStock retrieves the stock quantity for a specific product variant from the database.
// It returns the stock quantity as an integer and an error if any issues occur during the database query.
func (r *CartRepository) VariantStock(
	ctx context.Context,
	variantID int,
) (int, error) {
	if variantID <= 0 {
		return 0, fmt.Errorf("variant ID must be positive")
	}

	var stock int
	err := r.db.QueryRowContext(ctx, `
        SELECT stock_quantity
        FROM product_variant
        WHERE variant_id = ?
    `, variantID).Scan(&stock)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("variant not found")
	}
	if err != nil {
		return 0, fmt.Errorf("get variant stock: %w", err)
	}

	return stock, nil
}
