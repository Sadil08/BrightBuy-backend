package app

import "time"

// DateRange is inclusive of From and exclusive of To.
type DateRange struct {
	From time.Time
	To   time.Time
}

// QuarterlySalesRow is one quarter in the annual sales report.
type QuarterlySalesRow struct {
	Year       int    `json:"year"`
	Quarter    int    `json:"quarter"`
	SalesValue string `json:"salesValue"`
}

// TopSellingProductRow is one product in the top-selling report.
type TopSellingProductRow struct {
	ProductID   int64  `json:"productId"`
	ProductName string `json:"productName"`
	Quantity    int64  `json:"quantitySold"`
	Revenue     string `json:"revenue"`
}

// TopSellingProductsReport includes the effective date range so an all-time report is explicit.
type TopSellingProductsReport struct {
	From     *string                `json:"from"`
	To       *string                `json:"to"`
	Products []TopSellingProductRow `json:"products"`
}

// CategoryOrderCount is one category's distinct order count.
type CategoryOrderCount struct {
	CategoryID int64  `json:"categoryId"`
	Name       string `json:"name"`
	OrderCount int64  `json:"orderCount"`
}

// CategoryWiseReport carries the required explanation for the non-additive counts.
type CategoryWiseReport struct {
	Categories []CategoryOrderCount `json:"categories"`
	Note       string               `json:"note"`
}

// UpcomingDeliveryRow is one unfulfilled order with its estimated delivery.
type UpcomingDeliveryRow struct {
	OrderID       int64     `json:"orderId"`
	CustomerName  string    `json:"customerName"`
	CustomerEmail string    `json:"customerEmail"`
	DeliveryMode  string    `json:"deliveryMode"`
	Address       string    `json:"address"`
	EstimatedDate time.Time `json:"estimatedDate"`
	OrderStatus   string    `json:"orderStatus"`
}

// CustomerOrderPaymentRow is one customer's order/payment history row.
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
