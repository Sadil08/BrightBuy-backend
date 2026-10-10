package httpapi

import (
	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/shared/money"
)

// The staff API speaks the same dialect as the rest of the API (camelCase fields, prices as decimal
// strings — specs/global/01_TECH_STACK.md §3.5, openapi.yaml). The app layer works in integer cents,
// so these DTOs are the only place the two meet.

type variantCreateRequest struct {
	SKU          string                `json:"sku"`
	Price        money.Money           `json:"price"`
	OpeningStock int                   `json:"openingStock"`
	Attributes   []VariantAttributeDTO `json:"attributes"`
}

func (v variantCreateRequest) toInput() app.VariantInput {
	attrs := make([]app.VariantAttribute, 0, len(v.Attributes))
	for _, a := range v.Attributes {
		attrs = append(attrs, app.VariantAttribute{Name: a.Name, Value: a.Value})
	}
	return app.VariantInput{SKU: v.SKU, PriceCents: v.Price.Cents(), StockQuantity: v.OpeningStock, Attributes: attrs}
}

type productCreateRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	CategoryIDs []int64                `json:"categoryIds"`
	Variants    []variantCreateRequest `json:"variants"`
}

func (p productCreateRequest) toInput() app.ProductInput {
	variants := make([]app.VariantInput, 0, len(p.Variants))
	for _, v := range p.Variants {
		variants = append(variants, v.toInput())
	}
	return app.ProductInput{Name: p.Name, Description: p.Description, CategoryIDs: p.CategoryIDs, Variants: variants}
}

type productPatchRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// variantPatchRequest only carries price: stock is owned by 06-inventory (every change must leave a
// stock_movement audit row), so changing it through the catalogue API would bypass the audit trail.
type variantPatchRequest struct {
	Price *money.Money `json:"price"`
}

type categoryRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type adminProductResponse struct {
	ProductID   int64  `json:"productId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

type adminVariantResponse struct {
	VariantID int64       `json:"variantId"`
	ProductID int64       `json:"productId"`
	SKU       string      `json:"sku"`
	Price     money.Money `json:"price"`
	Active    bool        `json:"active"`
}

type adminCategoryResponse struct {
	CategoryID  int64  `json:"categoryId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}
