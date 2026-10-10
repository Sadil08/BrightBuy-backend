package domain

import "time"

type Status string

const StatusConfirmed Status = "Confirmed"

type PaymentStatus string

const (
	PaymentPending    PaymentStatus = "Pending"
	PaymentAuthorized PaymentStatus = "Authorized"
)

type DeliveryMode string

const (
	StorePickup      DeliveryMode = "StorePickup"
	StandardDelivery DeliveryMode = "StandardDelivery"
)

type PaymentMethod string

const (
	PaymentCOD  PaymentMethod = "COD"
	PaymentCard PaymentMethod = "Card"
)

type OrderItem struct {
	VariantID        int    `json:"variantId"`
	ProductName      string `json:"productName"`
	Quantity         int    `json:"quantity"`
	UnitPriceAtOrder string `json:"unitPriceAtOrder"`
}

type Delivery struct {
	Mode          DeliveryMode `json:"mode"`
	EstimatedDate string       `json:"estimatedDate"`
	EstimatedDays int          `json:"estimatedDays"`
}

// StatusEvent is one row of an order's audit trail (order_status_history, REQ-8.5). ChangedBy is only
// filled for staff views — customers see what happened and when, never which employee did it.
type StatusEvent struct {
	Status    Status    `json:"status"`
	ChangedAt time.Time `json:"changedAt"`
	ChangedBy string    `json:"changedBy,omitempty"`
}

type Order struct {
	ID            int           `json:"orderId"`
	Status        Status        `json:"status"`
	Items         []OrderItem   `json:"items"`
	Subtotal      string        `json:"subtotal"`
	TaxAmount     string        `json:"taxAmount"`
	DeliveryFee   string        `json:"deliveryFee"`
	TotalAmount   string        `json:"totalAmount"`
	Delivery      Delivery      `json:"delivery"`
	PaymentMethod PaymentMethod `json:"paymentMethod"`
	PaymentStatus PaymentStatus `json:"paymentStatus"`
	CreatedAt     time.Time     `json:"createdAt"`
	CustomerName  string        `json:"customerName,omitempty"` // staff views only
	History       []StatusEvent `json:"history,omitempty"`
}

type UnavailableLine struct {
	VariantID   int    `json:"variantId"`
	ProductName string `json:"productName"`
	Requested   int    `json:"requested"`
	Available   int    `json:"available"`
}
