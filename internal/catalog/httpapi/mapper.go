package httpapi

import (
	"brightbuy-backend/internal/catalog/domain"
	"brightbuy-backend/internal/shared/money"
	"sort"
)

// From my understanding, the toCategoryDTO function is a utility function that converts a domain.Category object into a CategoryDTO object. This is useful for mapping data from the domain layer to the HTTP API layer, ensuring that the data returned in API responses is in the correct format.
// That means that the toCategoryDTO function takes a domain.Category object as input and returns a CategoryDTO object with the same ID, Name, and Description fields. This allows for a clean separation between the domain model and the data transfer objects used in the API layer, making it easier to manage changes in either layer without affecting the other.
func toCategoryDTO(c domain.Category) CategoryDTO {
	return CategoryDTO{
		ID:          c.ID,
		Name:        c.Name,
		Description: c.Description,
	}

}

func toVariantAttributeDTO(attributes map[string]string) []VariantAttributeDTO {

	//maing sure that when ranging over a map that we wont iterate over a random order, we can create a slice of keys and sort it before iterating over the map. This way, we can ensure that the attributes are always returned in a consistent order.
	names := make([]string, 0, len(attributes)) //this creates a slice of strings called names with an initial length of 0 and a capacity equal to the length of the attributes map. This is done to avoid unnecessary memory allocations when appending to the slice later.
	for name := range attributes {
		names = append(names, name)
	}
	sort.Strings(names) //sort the names slice in ascending order. This ensures that the attributes are always returned in a consistent order, regardless of the order in which they were added to the map.
	// `:= []VariantAttributeDTO{}` on purpose, not `var variantAttributes []VariantAttributeDTO`:
	// that second form starts as a nil slice, and if `attributes` is empty, the loop below never
	// appends anything, so it STAYS nil — and encoding/json marshals a nil slice as `null`, not `[]`.
	// openapi.yaml declares `attributes` as a plain (non-nullable) array, so a variant with zero
	// attributes must still serialize as `"attributes":[]`, never `"attributes":null` — a strict
	// client doing `attributes.map(...)` would crash on null. Starting from an empty-but-non-nil
	// slice fixes it: zero iterations still leaves a `[]VariantAttributeDTO{}`, which marshals as `[]`.
	variantAttributes := []VariantAttributeDTO{}
	for _, name := range names { //the _ is a blank identifier, which is used when we don't need to use the index of the slice. In this case, we only care about the name of the attribute, so we can ignore the index.
		value := attributes[name]
		variantAttributes = append(variantAttributes, VariantAttributeDTO{
			Name:  name,
			Value: value,
		})
	}
	return variantAttributes
}

// toVariantDTO converts one domain.Variant into the shape a client receives over JSON. Most fields
// are a straight copy, but two are worth slowing down for:
//
//   - Price needs NO conversion, even though it might look like it should: domain.Variant.Price and
//     VariantDTO.Price are both already money.Money (we deliberately designed it that way back when
//     we built the Money type, specifically so a price never needs reformatting as it moves between
//     layers — just hand the same value straight across).
//   - Stock DOES need a conversion. domain.StockStatus is its own named type (`type StockStatus
//     string`, from stock_status.go) rather than a plain string, on purpose — so a StockStatus value
//     can't get mixed up with an arbitrary, unvalidated string elsewhere in the code. VariantDTO.
//     StockStatus, on the other hand, IS a plain string (that's what the JSON contract wants, and
//     httpapi doesn't need the extra type-safety domain cares about). Going from a named type to its
//     underlying type is never automatic in Go — you have to say explicitly "yes, treat this as a
//     plain string now" with a type CONVERSION: string(v.Stock). It looks exactly like a function
//     call, but it isn't one — string(...) here means "convert", not "invoke."
func toVariantDTO(v domain.Variant) VariantDTO {
	return VariantDTO{
		VariantID:   v.ID,
		SKU:         v.SKU,
		Price:       v.Price,
		StockStatus: string(v.Stock),
		Attributes:  toVariantAttributeDTO(v.Attributes),
	}
}

// toVariantDTOs converts a whole slice of domain.Variant into a slice of VariantDTO by calling
// toVariantDTO once per element. This "loop that builds a new slice by converting each item" shape
// comes up constantly once you're gluing two layers together — the exact same pattern repeats just
// below for categories, and again further down for a page of products.
func toVariantDTOs(variants []domain.Variant) []VariantDTO {
	dtos := make([]VariantDTO, 0, len(variants))
	for _, v := range variants {
		dtos = append(dtos, toVariantDTO(v))
	}
	return dtos
}

// toCategoryDTOs is the identical "convert each element" pattern as toVariantDTOs above, just for
// categories instead of variants — once you've seen the shape once, it's the same every time.
func toCategoryDTOs(categories []domain.Category) []CategoryDTO {
	dtos := make([]CategoryDTO, 0, len(categories))
	for _, c := range categories {
		dtos = append(dtos, toCategoryDTO(c))
	}
	return dtos
}

// toProductDTO converts a full domain.Product — as returned by CatalogService.GetProduct, the
// product-DETAIL use case — into the shape GET /products/{productId} promises. Nothing is computed
// here; Categories and Variants are just converted element-by-element via the two helpers above.
func toProductDTO(p domain.Product, imageBaseURL string) ProductDTO {
	images := make([]ImageDTO, 0, len(p.Images))
	for i, img := range p.Images {
		images = append(images, ImageDTO{ImageID: img.ID, URL: imageBaseURL + "/" + img.ObjectKey, SortOrder: i, IsPrimary: i == 0})
	}
	return ProductDTO{
		Images:      images,
		ProductID:   p.ID,
		Name:        p.Name,
		Description: p.Description,
		Categories:  toCategoryDTOs(p.Categories),
		Variants:    toVariantDTOs(p.Variants),
	}
}

// toProductSummaryDTO is different from every function above it in this file: it doesn't just copy
// fields across, it COMPUTES two values that don't exist anywhere on domain.Product itself. The
// paginated GET /products list (openapi.yaml's ProductSummary shape) wants one "priceFrom" and one
// "stockStatus" per product, not a full variant list — so we derive both by looking across all of
// this product's variants:
//
//   - priceFrom: the cheapest variant's price ("starting at $X"), found with the same
//     "walk the slice, keep whichever is best so far" pattern you'd use to find a max or min
//     anywhere — track the best candidate seen so far (priceFrom), and replace it whenever a better
//     one (cheaper, via Money.LessThan) comes along. i == 0 seeds it with the very first variant, since
//     there's no "so far" to compare against yet on the first iteration.
//   - stockStatus: "IN_STOCK" if ANY variant is in stock, "OUT_OF_STOCK" only if EVERY variant is out
//     of stock — a shopper doesn't care that the Black one sold out if White is still available.
//
// Worth noticing what ISN'T here: no `if len(p.Variants) == 0 { ... }` special case for a product
// with no variants (plan.md flags this as a data issue that should "never happen," but shouldn't
// crash if it somehow does). The loop below simply never finds an in-stock variant when there are no
// variants to look at, so stockStatus quietly falls through to its "OUT_OF_STOCK" starting value, and
// priceFrom stays at Money's zero value ("0.00"). Same lesson as the SQL WHERE clauses back in
// ProductRepository: letting a loop's natural behavior handle the empty case is often simpler and
// more robust than writing an explicit branch for it.
func toProductSummaryDTO(p domain.Product) ProductSummaryDTO {
	var priceFrom money.Money
	stockStatus := "OUT_OF_STOCK"

	for i, v := range p.Variants {
		if i == 0 || v.Price.LessThan(priceFrom) {
			priceFrom = v.Price
		}
		if v.Stock == domain.InStock {
			stockStatus = "IN_STOCK"
		}
	}

	return ProductSummaryDTO{
		ProductID:   p.ID,
		Name:        p.Name,
		PriceFrom:   priceFrom,
		StockStatus: stockStatus,
	}
}

// toProductSummaryDTOs: the same "convert each element" pattern one more time, for a whole page of
// products at once.
func toProductSummaryDTOs(products []domain.Product) []ProductSummaryDTO {
	dtos := make([]ProductSummaryDTO, 0, len(products))
	for _, p := range products {
		dtos = append(dtos, toProductSummaryDTO(p))
	}
	return dtos
}

// toProductListDTO builds the full GET /products response "envelope" — the actual page of products
// (Items) plus the pagination metadata a client needs to render page controls (Page). page and size
// are just handed back as given (whatever the client asked for, or the service's clamped defaults);
// total comes from ProductRepository.List's second return value, the COUNT(*) result.
func toProductListDTO(products []domain.Product, page, size, total int) ProductListDTO {
	return ProductListDTO{
		Items: toProductSummaryDTOs(products),
		Page: PageDTO{
			Page:  page,
			Size:  size,
			Total: total,
		},
	}
}
