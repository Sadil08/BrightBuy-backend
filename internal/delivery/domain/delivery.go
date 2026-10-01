package domain

import "time"

type DeliveryMode string

const (
	StorePickup      DeliveryMode = "StorePickup"
	StandardDelivery DeliveryMode = "StandardDelivery"
)

func (mode DeliveryMode) Valid() bool {
	return mode == StorePickup || mode == StandardDelivery
}

type LineInput struct {
	VariantID int `json:"variant_id"`
	Quantity  int `json:"quantity"`
}

type Estimate struct {
	Mode          DeliveryMode
	EstimatedDays int
	EstimatedDate time.Time
}
