package mysql

// the database layer for the shopping cart in backend.

import (
	"context"
	"database/sql"
	"fmt" // for error formatting

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
2. Find the customer's cart (none yet = empty cart; reads never write)
3. Remember the cart ID
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

	cart := &domain.Cart{
		CustomerID: customerID,
		Items:      make([]domain.CartItem, 0),
	}

	// Reading never writes: a customer with no cart row yet just gets an empty cart (cartId 0). The
	// row is created by the first UpsertLine, so GET /cart can't create rows or burn auto-increment ids.
	err := r.db.QueryRowContext(ctx,
		`SELECT cart_id FROM cart WHERE customer_id = ?`,
		customerID,
	).Scan(&cart.ID)
	if err == sql.ErrNoRows {
		return cart, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find cart: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
        SELECT ci.cart_item_id, ci.variant_id, ci.quantity
        FROM cart c
        JOIN cart_item ci ON ci.cart_id = c.cart_id
        WHERE c.customer_id = ?
        ORDER BY ci.cart_item_id
    `, customerID)
	if err != nil {
		return nil, fmt.Errorf("query cart items: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item domain.CartItem

		if err := rows.Scan(
			&item.ID,
			&item.VariantID,
			&item.Quantity,
		); err != nil {
			return nil, fmt.Errorf("scan cart item: %w", err)
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
	cartID, err := r.ensureCartID(ctx, customerID)
	if err != nil {
		return nil, err
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

// CustomerIDForUser maps the authenticated user_account_id (from the JWT) to its customer profile.
// Returns ErrNotCustomer when the account has no customer row (staff/manager/admin accounts).
func (r *CartRepository) CustomerIDForUser(ctx context.Context, userAccountID int) (int, error) {
	var customerID int
	err := r.db.QueryRowContext(ctx, `
        SELECT customer_id
        FROM customer
        WHERE user_account_id = ?
    `, userAccountID).Scan(&customerID)
	if err == sql.ErrNoRows {
		return 0, domain.ErrNotCustomer
	}
	if err != nil {
		return 0, fmt.Errorf("get customer for user: %w", err)
	}
	return customerID, nil
}

// FindLineByID looks a cart line up by its own id, scoped to the caller in the query itself
// (SEC-CART-1, 02_SECURITY_BASELINE.md §6.2): another customer's line is simply "not found".
// Returns nil, nil when there is no such line for this customer.
func (r *CartRepository) FindLineByID(
	ctx context.Context,
	customerID, cartItemID int,
) (*domain.CartItem, error) {
	var item domain.CartItem
	err := r.db.QueryRowContext(ctx, `
        SELECT ci.cart_item_id, ci.variant_id, ci.quantity
        FROM cart c
        JOIN cart_item ci ON ci.cart_id = c.cart_id
        WHERE c.customer_id = ? AND ci.cart_item_id = ?
    `, customerID, cartItemID).Scan(&item.ID, &item.VariantID, &item.Quantity)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find cart line by id: %w", err)
	}
	return &item, nil
}

// ensureCartID returns the customer's cart id, creating the row only if it doesn't exist yet. It
// SELECTs first so the common case (cart already exists) doesn't run an INSERT that would burn an
// auto-increment id; the unique key on customer_id plus ON DUPLICATE KEY keeps two concurrent first
// writes safe (both end up with the same row).
func (r *CartRepository) ensureCartID(ctx context.Context, customerID int) (int, error) {
	var cartID int
	err := r.db.QueryRowContext(ctx, `SELECT cart_id FROM cart WHERE customer_id = ?`, customerID).Scan(&cartID)
	if err == nil {
		return cartID, nil
	}
	if err != sql.ErrNoRows {
		return 0, fmt.Errorf("find cart: %w", err)
	}
	if _, err := r.db.ExecContext(ctx, `
        INSERT INTO cart (customer_id) VALUES (?)
        ON DUPLICATE KEY UPDATE cart_id = LAST_INSERT_ID(cart_id)
    `, customerID); err != nil {
		return 0, fmt.Errorf("create cart: %w", err)
	}
	if err := r.db.QueryRowContext(ctx, `SELECT cart_id FROM cart WHERE customer_id = ?`, customerID).Scan(&cartID); err != nil {
		return 0, fmt.Errorf("find cart: %w", err)
	}
	return cartID, nil
}

// ClearItemsInTx empties the customer's cart inside the caller's transaction, so checkout can place
// the order and clear the cart atomically (plan 04 §5.1). The cart row itself stays.
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
