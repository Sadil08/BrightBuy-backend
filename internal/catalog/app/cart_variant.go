package app

import "brightbuy-backend/internal/shared/money"

// CartVariant is internal data for backend cart logic. It is not an HTTP response DTO.
type CartVariant struct {
	ID            int         `json:"-"`
	ProductName   string      `json:"-"`
	Price         money.Money `json:"-"`
	StockQuantity int         `json:"-"`
	Available     bool        `json:"-"`
}
