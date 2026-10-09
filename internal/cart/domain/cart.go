package domain

import (
	"fmt"
	"strconv"
)

// Money stores USD cents internally but serialises as a decimal string ("10.50") — the OpenAPI
// `Money` schema and 01_TECH_STACK.md §3 forbid money as a JSON number.
type Money int64

func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}

func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(m.String())), nil
}

// Cart and CartItem JSON field names follow specs/openapi/openapi.yaml (camelCase).
type Cart struct {
	ID         int        `json:"cartId"`
	CustomerID int        `json:"-"` // internal only; the client never needs to see or send it
	Items      []CartItem `json:"items"`
	Subtotal   Money      `json:"subtotal"`
}

type CartItem struct {
	ID           int    `json:"cartItemId"`
	VariantID    int    `json:"variantId"`
	ProductName  string `json:"productName"`
	UnitPrice    Money  `json:"unitPrice"`
	Quantity     int    `json:"quantity"`
	LineTotal    Money  `json:"lineTotal"`
	StockWarning bool   `json:"stockWarning"` // advisory only (REQ-3.4)
	Unavailable  bool   `json:"unavailable"`  // variant/product deactivated; excluded from subtotal
}
