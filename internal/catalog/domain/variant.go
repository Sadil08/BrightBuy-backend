package domain

import "brightbuy-backend/internal/shared/money"

// Variant is one purchasable configuration of a Product — e.g. "Black / 128GB" — with its own SKU,
// price and stock. Attributes is a simple name -> value map (e.g. {"Color": "Black", "Storage":
// "128GB"}) that the repository builds by joining variant_attribute -> attribute_value ->
// attribute_name (the EAV tables described in specs/global/00_OVERVIEW.md §4); this package doesn't
// know or care that those three tables exist, it just receives the assembled map.
//
// A note on the money.Money import: specs/global/06_ENGINEERING_STANDARDS.md §7 says domain
// "imports nothing but the Go standard library." money.Money is the one deliberate exception: it's a
// pure value type with zero infrastructure knowledge (no SQL, no HTTP, no framework — see its own
// doc comment), used identically by every feature's domain layer. Duplicating it inside each
// feature's domain package would just recreate the float-precision bug this type exists to prevent,
// one copy-paste at a time. Worth flagging if this project's convention should say so explicitly.
type Variant struct {
	ID         int
	SKU        string
	Price      money.Money
	Stock      StockStatus
	Attributes map[string]string
}
