package domain

// StockStatus is the ONLY stock information this module ever hands to a caller outside its own
// mysql package. There is deliberately no "quantity" field anywhere in this package — the exact
// stock_quantity column is computed down to this two-value enum inside the repository (via the
// fn_is_variant_in_stock SQL function) and never travels any further up the call chain, so there is
// no code path left by which it could accidentally leak into an API response later
// (FR-CATALOG-7 / SEC-CATALOG-1: exact stock quantity must never reach a customer).
//
// `type StockStatus string` is a "named type": StockStatus and string hold the same data at
// runtime, but they are different types to the compiler. A function that takes a StockStatus
// parameter cannot be called with a bare string literal like "IN_STOCK" without a conversion — that
// small amount of friction is the point, since it stops an arbitrary string from being mistaken for
// a validated status elsewhere in the code.
type StockStatus string

const (
	InStock    StockStatus = "IN_STOCK"
	OutOfStock StockStatus = "OUT_OF_STOCK"
)
