package domain

type VariantStock struct {
	VariantID     int
	SKU           string
	ProductName   string
	StockQuantity int
}

/* Represents a staff inventory result.
Includes the exact stock quantity.
Is different from the public catalog model, which must
expose only IN_STOCK or OUT_OF_STOCK.*/
