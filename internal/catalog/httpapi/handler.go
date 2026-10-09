package httpapi

import (
	"brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/shared/httpx"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type CatalogHandler struct {
	service *app.CatalogService
}

func NewCatalogHandler(service *app.CatalogService) *CatalogHandler {
	return &CatalogHandler{service: service}
}

// setCacheHeaders applies the Cache-Control half of the cache policy plan.md §1 calls for on this
// feature's read endpoints: products/categories only change via a staff action (07-admin-catalog),
// not on every request, so a browser or intermediary cache can safely reuse a response for up to 60
// seconds instead of re-fetching identical data. Called only on the success path in each handler
// below — a 404/500 shouldn't be cached the same way a stable, found resource is.
//
// The other half — a weak ETag derived from `updated_at` — is implemented separately, only on
// GetProduct below (see its own comment): categories and the product LIST don't have a single
// natural "last modified" row to key a collection-wide ETag off of the way one product's detail
// view does, and `category` doesn't even have an `updated_at` column today.
func setCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=60")
}

func (h *CatalogHandler) ListCategories(w http.ResponseWriter, r *http.Request) {
	// 1. call the service, using the request's context (r.Context()) — same `ctx context.Context`
	//    first argument every service/repository method you've already written expects.
	categories, err := h.service.ListCategories(r.Context())
	// 2. if it returns an error, write it with httpx.WriteError(w, http.StatusInternalServerError,
	//    "INTERNAL_ERROR", "something went wrong") and `return` immediately — don't fall through to
	//    step 3 if something went wrong.
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	// 3. otherwise, convert the []domain.Category you got back into DTOs (you already wrote the
	//    function for this!) and send them with httpx.WriteJSON(w, http.StatusOK, ...).
	setCacheHeaders(w)
	httpx.WriteJSON(w, http.StatusOK, toCategoryDTOs(categories))

}

func (h *CatalogHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	// r.URL.Query() parses the "?q=...&categoryId=...&page=...&size=..." part of the URL into a
	// map-like value, and .Get("q") reads one param by name — "" if it wasn't present at all.
	// Query params are ALWAYS optional/absent-able, unlike a path param like {productId}, so every
	// read here has to account for "what if this wasn't sent?"
	query := r.URL.Query()

	filter := app.ListFilter{
		Query: query.Get("q"),
	}

	// page/size: if the client sent a valid positive number, use it; otherwise just leave the
	// field at its zero value (0) — ListFilter.Normalized() (in app/catalog_service.go) already
	// knows how to turn "0, i.e. not specified" into the real default, so we don't duplicate that
	// decision here. We only bother converting the string AT ALL if something was actually sent.
	if raw := query.Get("page"); raw != "" {
		if page, err := strconv.Atoi(raw); err == nil {
			filter.Page = page
		}
		// if err != nil here, we just silently ignore the bad value and fall back to the default,
		// rather than rejecting the whole request over one malformed param — same "be lenient about
		// non-critical input" philosophy as an unknown categoryId just returning empty results.
	}
	if raw := query.Get("size"); raw != "" {
		if size, err := strconv.Atoi(raw); err == nil {
			filter.PageSize = size
		}
	}

	// categoryId is different: it's an *int, not an int, because ListFilter needs to tell "no
	// category filter at all" (nil) apart from "filter by category 0" (a real, if unusual, value).
	// A plain int can't represent "absent" — it would always be SOME number, even 0 by default.
	if raw := query.Get("categoryId"); raw != "" {
		if categoryID, err := strconv.Atoi(raw); err == nil {
			filter.CategoryID = &categoryID // &categoryID: "the address of this variable" — a pointer to it
		}
	}

	filter = filter.Normalized() // same clamping CatalogService.ListProducts applies internally —
	// doing it here too (not just inside the service) means filter.Page/filter.PageSize below
	// already hold the REAL values that will be used, for the response's pagination metadata.

	products, total, err := h.service.ListProducts(r.Context(), filter)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	setCacheHeaders(w)
	httpx.WriteJSON(w, http.StatusOK, toProductListDTO(products, filter.Page, filter.PageSize, total))
}

func (h *CatalogHandler) GetProduct(w http.ResponseWriter, r *http.Request) {
	// chi.URLParam reads whatever actual text filled in the {productId} part of the URL for THIS
	// request (e.g. someone requesting /products/42 gets "42" here). It always comes back as a
	// string — the URL is just text, chi doesn't know "productId" is supposed to be a number.
	rawID := chi.URLParam(r, "productId")

	// strconv.Atoi ("ASCII to integer") tries to convert that string into an int. It returns two
	// values: the converted number, and an error if the string wasn't actually a valid number (e.g.
	// someone requested /products/banana). This is a DIFFERENT kind of bad input than "not found" —
	// it's not a valid request at all, so it's a 400 Bad Request, not a 404.
	id, err := strconv.Atoi(rawID)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "INVALID_ID", "product id must be a number")
		return // stop here — don't try to look up a product with a ID we couldn't even parse
	}

	// now we have a real int, so we can actually ask the service for this product.
	product, err := h.service.GetProduct(r.Context(), id)
	if err != nil {
		// errors.Is checks whether `err` specifically IS (or wraps) app.ErrNotFound — the sentinel
		// error the repository returns when no matching product exists. This lets us tell "genuinely
		// doesn't exist" (404) apart from "something actually broke" (500), using the SAME err
		// variable, instead of two separate signals.
		if errors.Is(err, app.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "PRODUCT_NOT_FOUND", "no such product")
			return
		}
		// any other kind of error (e.g. the database connection dropped) — we don't know exactly
		// what went wrong, so we don't leak internal details to the client, just a generic 500.
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	// A weak ETag (the `W/` prefix) derived from product.UpdatedAt — which, per
	// mysql.ProductRepository.GetByID, is already the MAX of the product row's own timestamp and
	// every one of its variants' timestamps, so a stock/price change on any variant invalidates this
	// just as much as editing the product's name would. "Weak" is the honest label here: this isn't
	// a hash of the exact response bytes, just a proxy for "has anything about this product changed
	// since this timestamp" — perfectly adequate for deciding whether to resend an identical page,
	// not something to use anywhere a byte-exact comparison would matter.
	//
	// CheckETag both sets the response header AND checks the request's If-None-Match header — if a
	// client already has this exact version cached, this returns true having already written a
	// 304 (no body at all), and we stop here instead of re-building/re-sending the same JSON again.
	etag := fmt.Sprintf(`W/"%d"`, product.UpdatedAt.Unix())
	if httpx.CheckETag(w, r, etag) {
		return
	}

	// GetProduct returns a *domain.Product — a POINTER to one, not the value itself (check its
	// signature in app/catalog_service.go). toProductDTO wants a plain domain.Product, not a pointer
	// to one, so *product ("dereference product") says "give me the actual value this pointer points
	// to." Note the * is in front of a VARIABLE here, not a type — same symbol, different meaning
	// depending on where it shows up, which is a little confusing at first.
	setCacheHeaders(w)
	httpx.WriteJSON(w, http.StatusOK, toProductDTO(*product))
}

// RegisterRoutes wires each handler method to the actual URL path it answers for — this is the
// piece that turns chi.URLParam(r, "productId") above from "a string that happens to be named
// productId" into something real: chi only fills that in because the path pattern below has a
// `{productId}` segment at the matching position. Without this function being called, none of the
// three handlers above are reachable by any HTTP request at all — they'd just be ordinary Go
// methods, sitting unused.
//
// r chi.Router is the same router type cmd/api/main.go already builds with chi.NewRouter() for the
// /healthz and /readyz routes — this function doesn't create a NEW router, it adds three more routes
// onto whichever one main.go hands it.
func RegisterRoutes(r chi.Router, h *CatalogHandler) {
	r.Get("/categories", h.ListCategories)
	r.Get("/products", h.ListProducts)
	r.Get("/products/{productId}", h.GetProduct)
}
