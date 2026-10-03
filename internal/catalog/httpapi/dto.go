package httpapi

import (
	"brightbuy-backend/internal/shared/money"
)

// ProductDTO is the JSON representation of a product, as returned by the HTTP API.
type ProductDTO struct {
	ProductID   int           `json:"productId"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Categories  []CategoryDTO `json:"categories"`
	Variants    []VariantDTO  `json:"variants"`
	//add image field later
}

type CategoryDTO struct {
	ID          int    `json:"categoryId"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type VariantAttributeDTO struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type VariantDTO struct {
	VariantID   int                   `json:"variantId"`
	SKU         string                `json:"sku"`
	Price       money.Money           `json:"price"`
	StockStatus string                `json:"stockStatus"`
	Attributes  []VariantAttributeDTO `json:"attributes"`
}

type ProductSummaryDTO struct {
	ProductID   int         `json:"productId"`
	Name        string      `json:"name"`
	PriceFrom   money.Money `json:"priceFrom"`
	StockStatus string      `json:"stockStatus"`
}

type PageDTO struct {
	Page  int `json:"page"`
	Size  int `json:"size"`
	Total int `json:"total"`
}

type ProductListDTO struct {
	Items []ProductSummaryDTO `json:"items"`
	Page  PageDTO             `json:"page"`
}
