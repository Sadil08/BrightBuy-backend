// Package money defines the one Money value type every feature uses for prices and totals
// (specs/global/06_ENGINEERING_STANDARDS.md §5: "never float64 for a price or total, anywhere").
//
// Why not float64? A float can't represent 0.10 exactly in binary, so repeated arithmetic on prices
// (summing a cart, applying a discount) silently accumulates rounding error — the kind of bug that's
// invisible in testing and shows up as a one-cent mismatch in production. Money instead stores the
// amount as an integer count of "minor units" (cents, for a 2-decimal-place currency) — integers add
// and subtract exactly, with no rounding error possible.
//
// This package imports only the standard library on purpose. domain packages across every feature
// (catalog, cart, ordering, ...) are only allowed to import the Go standard library
// (specs/global/06_ENGINEERING_STANDARDS.md §7) — Money has to be safe for them to depend on, so it
// can never gain a database or HTTP dependency itself.
package money

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
)

// Money is an amount of money with exactly 2 decimal places, stored as an integer number of cents.
// The zero value, Money{}, is a valid "0.00" — no constructor call required.
type Money struct {
	cents int64
}

// LessThan reports whether m is a smaller amount than other — e.g. for finding the cheapest variant
// of a product. A direct `m < other` doesn't compile for a struct type in Go (only ==/!= work on
// structs, never ordering operators), and m.cents/other.cents aren't reachable from outside this
// package since the field is unexported — so this method exists to be the one place that comparison
// happens, rather than exposing the raw cents count just so some other package can do it itself.
func (m Money) LessThan(other Money) bool {
	return m.cents < other.cents
}

// FromCents builds a Money directly from an integer cent count — useful when you already have whole
// cents (e.g. a unit test) and want to skip string parsing.
func FromCents(cents int64) Money {
	return Money{cents: cents}
}

// Parse converts a decimal string like "19.99", "20", or "-3.5" into a Money. It rejects more than 2
// decimal places rather than silently rounding, since silently rounding a price is exactly the kind
// of bug this whole type exists to prevent.
func Parse(s string) (Money, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Money{}, fmt.Errorf("money: cannot parse empty string")
	}

	negative := false
	if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	}

	wholePart, fracPart, hasFraction := strings.Cut(s, ".")
	if wholePart == "" {
		wholePart = "0"
	}
	if !hasFraction {
		fracPart = "00"
	}
	if len(fracPart) > 2 {
		return Money{}, fmt.Errorf("money: %q has more than 2 decimal places", s)
	}
	for len(fracPart) < 2 {
		fracPart += "0" // pad "5" -> "50" so "5.5" means 5 dollars 50 cents, not 5 dollars 5 cents
	}

	whole, err := strconv.ParseInt(wholePart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("money: invalid amount %q: %w", s, err)
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("money: invalid amount %q: %w", s, err)
	}

	cents := whole*100 + frac
	if negative {
		cents = -cents
	}
	return Money{cents: cents}, nil
}

// String formats the amount back to a decimal string, e.g. "19.99" or "-3.50" — always 2 decimal
// places, matching the MySQL DECIMAL(10,2) columns this type reads from
// (specs/global/07_SQL_DATABASE_STANDARDS.md §1: "Money: DECIMAL(10,2), never FLOAT/DOUBLE").
func (m Money) String() string {
	cents := m.cents
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// MarshalJSON makes Money satisfy encoding/json.Marshaler, so json.Marshal (and anything that calls
// it, like httpx.WriteJSON) writes it as a JSON string — "19.99" — never a JSON number. This is
// specs/global/01_TECH_STACK.md §3.5's rule: a JSON number is a float in every client language, which
// would smuggle the exact float-precision problem this type exists to avoid right back in on the
// frontend's side of the wire.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

// UnmarshalJSON is the reverse of MarshalJSON, so a Money field can also appear in a request body.
// Note the pointer receiver (*Money) here, unlike every method above: json.Unmarshal needs to write a
// new value INTO the Money the caller already has, not compute a new one to return.
func (m *Money) UnmarshalJSON(data []byte) error {
	parsed, err := Parse(strings.Trim(string(data), `"`))
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// Scan makes Money satisfy database/sql.Scanner, so a repository can read a DECIMAL(10,2) column
// straight into a Money field: `row.Scan(&variant.Price)` just works, no manual conversion at every
// call site. The go-sql-driver/mysql driver hands DECIMAL columns to Scan as []byte (it's sent over
// the wire as text, not a native numeric type), so that's the case that matters; string and nil are
// handled too since database/sql.Scanner is documented to receive one of a fixed set of types.
func (m *Money) Scan(value any) error {
	if value == nil {
		*m = Money{}
		return nil
	}

	var s string
	switch v := value.(type) {
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("money: cannot scan %T into Money", value)
	}

	parsed, err := Parse(s)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// Value makes Money satisfy database/sql/driver.Valuer, the write-side counterpart to Scan — needed
// once a later feature (checkout, admin-catalog) starts INSERTing/UPDATEing a Money value as a query
// argument. Returning the decimal string lets MySQL parse it directly into the DECIMAL column.
func (m Money) Value() (driver.Value, error) {
	return m.String(), nil
}
