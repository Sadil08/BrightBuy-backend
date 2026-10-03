package domain

import "time"

// DateRange is inclusive of From and exclusive of To.
type DateRange struct {
	From time.Time
	To   time.Time
}

type QuarterlySalesRow struct {
	Year       int    `json:"year"`
	Quarter    int    `json:"quarter"`
	SalesValue string `json:"salesValue"`
}

type TopSellingProductRow struct {
	ProductID   int64  `json:"productId"`
	ProductName string `json:"productName"`
	Quantity    int64  `json:"quantitySold"`
	Revenue     string `json:"revenue"`
}

type CategoryOrderRow struct {
	CategoryID   int64  `json:"categoryId"`
	CategoryName string `json:"categoryName"`
	OrderCount   int64  `json:"orderCount"`
}

type UpcomingDeliveryRow struct {
	OrderID       int64     `json:"orderId"`
	CustomerName  string    `json:"customerName"`
	CustomerEmail string    `json:"customerEmail"`
	DeliveryMode  string    `json:"deliveryMode"`
	Address       string    `json:"address"`
	EstimatedDate time.Time `json:"estimatedDate"`
	OrderStatus   string    `json:"orderStatus"`
}

type CustomerOrderPaymentRow struct {
	CustomerID    int64     `json:"customerId"`
	CustomerName  string    `json:"customerName"`
	CustomerEmail string    `json:"customerEmail"`
	OrderID       int64     `json:"orderId"`
	OrderDate     time.Time `json:"orderDate"`
	OrderStatus   string    `json:"orderStatus"`
	SalesValue    string    `json:"salesValue"`
	PaymentMethod string    `json:"paymentMethod"`
	PaymentStatus string    `json:"paymentStatus"`
	PaymentAmount string    `json:"paymentAmount"`
}
