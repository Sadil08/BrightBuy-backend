package domain

type Money int64 //stores USD cents

type Cart struct {
	ID         int
	CustomerID int
	Items      []CartItem
	Subtotal   Money
}

type CartItem struct {
	ID           int
	VariantID    int
	ProductName  string
	UnitPrice    Money
	Quantity     int
	LineTotal    Money
	StockWarning bool
	Unavailable  bool
}
