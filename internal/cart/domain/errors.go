package domain

import (
	"errors"
	"fmt"
)

var (
	// ErrVariantNotFound: the variant doesn't exist or is no longer sold (deactivated).
	ErrVariantNotFound = errors.New("variant not found")
	// ErrLineNotFound: no such cart line for THIS customer. Deliberately the same error whether the
	// line doesn't exist or belongs to someone else, so ownership can't be probed (SEC-CART-1).
	ErrLineNotFound = errors.New("cart item not found")
	// ErrNotCustomer: the authenticated account has no customer profile (staff/admin), so no cart.
	ErrNotCustomer = errors.New("account has no customer profile")
	// ErrInvalidInput: the request was well-formed but breaks a cart rule (bad quantity, too many
	// merge items). Maps to 400, never 500.
	ErrInvalidInput = errors.New("invalid cart input")
)

// MaxLineQuantity bounds one cart line. Without it a merge (which doesn't check stock) could write
// a quantity that overflows the INT column and surface as a 500. Keep in sync with the `lte=` tag
// on the httpapi DTOs.
const MaxLineQuantity = 1000

// StockExceededError carries the available count so the client can surface it (AC-CART-2).
type StockExceededError struct {
	Requested int
	Available int
}

func (e *StockExceededError) Error() string {
	return fmt.Sprintf("requested quantity %d exceeds available stock %d", e.Requested, e.Available)
}
