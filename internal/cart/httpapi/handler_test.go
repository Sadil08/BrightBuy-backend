package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cartapp "brightbuy-backend/internal/cart/app"
	"brightbuy-backend/internal/cart/domain"
	"brightbuy-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
)

// stubStore is a minimal in-memory cartapp.Store: one stock level for every variant, lines keyed
// by customer then variant, and user 7 -> customer 1, user 8 -> customer 2, anyone else has no
// customer profile (like a staff account).
type stubStore struct {
	lines  map[int]map[int]int
	ids    map[int]map[int]int
	nextID int
}

const stubStock = 5

func newStub() *stubStore {
	return &stubStore{lines: map[int]map[int]int{}, ids: map[int]map[int]int{}, nextID: 1}
}

func (f *stubStore) Get(_ context.Context, c int) (*domain.Cart, error) {
	cart := &domain.Cart{CustomerID: c, Items: []domain.CartItem{}}
	for v, q := range f.lines[c] {
		cart.Items = append(cart.Items, domain.CartItem{ID: f.ids[c][v], VariantID: v, Quantity: q})
	}
	return cart, nil
}
func (f *stubStore) UpsertLine(ctx context.Context, c, v, q int) (*domain.Cart, error) {
	if f.lines[c] == nil {
		f.lines[c], f.ids[c] = map[int]int{}, map[int]int{}
	}
	if _, ok := f.lines[c][v]; !ok {
		f.ids[c][v] = f.nextID
		f.nextID++
	}
	f.lines[c][v] = q
	return f.Get(ctx, c)
}
func (f *stubStore) FindLine(_ context.Context, c, v int) (*domain.CartItem, error) {
	if q, ok := f.lines[c][v]; ok {
		return &domain.CartItem{ID: f.ids[c][v], VariantID: v, Quantity: q}, nil
	}
	return nil, nil
}
func (f *stubStore) FindLineByID(_ context.Context, c, id int) (*domain.CartItem, error) {
	for v, lid := range f.ids[c] {
		if lid == id {
			return &domain.CartItem{ID: id, VariantID: v, Quantity: f.lines[c][v]}, nil
		}
	}
	return nil, nil
}
func (f *stubStore) DeleteLine(_ context.Context, c, v int) error {
	delete(f.lines[c], v)
	delete(f.ids[c], v)
	return nil
}
func (f *stubStore) VariantStock(_ context.Context, v int) (int, error) {
	if v == 404 {
		return 0, domain.ErrVariantNotFound
	}
	return stubStock, nil
}
func (f *stubStore) CustomerIDForUser(_ context.Context, userID int) (int, error) {
	switch userID {
	case 7:
		return 1, nil
	case 8:
		return 2, nil
	}
	return 0, domain.ErrNotCustomer
}

// newServer wires the REAL routes + real auth middleware, so 401/403 gating is tested too.
func newServer(t *testing.T) (*httptest.Server, *auth.TokenIssuer) {
	t.Helper()
	issuer := auth.NewTokenIssuer("test-signing-key-test-signing-key-123")
	h := NewHandler(cartapp.NewService(newStub()))
	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) { RegisterRoutes(api, h, issuer) })
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, issuer
}

func do(t *testing.T, srv *httptest.Server, issuer *auth.TokenIssuer, userID int, role, method, path, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+"/api/v1"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != 0 {
		tok, err := issuer.IssueAccessToken(userID, role, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: auth.AccessTokenCookie, Value: tok})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAuthGating(t *testing.T) {
	srv, iss := newServer(t)
	if s, _ := do(t, srv, iss, 0, "", "GET", "/cart", ""); s != 401 {
		t.Errorf("no session: got %d, want 401", s)
	}
	if s, _ := do(t, srv, iss, 99, "ADMIN", "GET", "/cart", ""); s != 403 {
		t.Errorf("non-customer role: got %d, want 403", s)
	}
	if s, _ := do(t, srv, iss, 99, "CUSTOMER", "GET", "/cart", ""); s != 403 {
		t.Errorf("customer role but no customer profile: got %d, want 403", s)
	}
	if s, _ := do(t, srv, iss, 7, "CUSTOMER", "GET", "/cart", ""); s != 200 {
		t.Errorf("customer: got %d, want 200", s)
	}
}

func TestAddItemValidationAndErrors(t *testing.T) {
	srv, iss := newServer(t)
	cases := []struct {
		name, body string
		want       int
		code       string
	}{
		{"ok", `{"variantId":1,"quantity":2}`, 200, ""},
		{"extra price field is ignored", `{"variantId":1,"quantity":2,"price":"0.01"}`, 200, ""},
		{"snake_case field is not accepted", `{"variant_id":1,"quantity":2}`, 400, "VALIDATION_FAILED"},
		{"quantity 0", `{"variantId":1,"quantity":0}`, 400, "VALIDATION_FAILED"},
		{"quantity negative", `{"variantId":1,"quantity":-1}`, 400, "VALIDATION_FAILED"},
		{"quantity over max", `{"variantId":1,"quantity":1001}`, 400, "VALIDATION_FAILED"},
		{"malformed JSON", `{`, 400, "INVALID_BODY"},
		{"over stock", `{"variantId":1,"quantity":6}`, 400, "STOCK_EXCEEDED"},
		{"unknown variant", `{"variantId":404,"quantity":1}`, 404, "VARIANT_NOT_FOUND"},
	}
	for _, c := range cases {
		s, out := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/items", c.body)
		if s != c.want || (c.code != "" && out["code"] != c.code) {
			t.Errorf("%s: got %d %v, want %d %s", c.name, s, out, c.want, c.code)
		}
	}
}

func TestResponseShapeFollowsOpenAPI(t *testing.T) {
	srv, iss := newServer(t)
	_, out := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/items", `{"variantId":1,"quantity":2}`)
	for _, k := range []string{"cartId", "items", "subtotal"} {
		if _, ok := out[k]; !ok {
			t.Errorf("missing %q in %v", k, out)
		}
	}
	if _, leaked := out["CustomerID"]; leaked {
		t.Error("customer id must not be exposed")
	}
	if out["subtotal"] != "0.00" { // stub has no prices; the point is it's a decimal STRING, not a number
		t.Errorf("subtotal = %#v, want string \"0.00\"", out["subtotal"])
	}
}

func TestOwnershipOnPatchAndDelete(t *testing.T) { // SEC-CART-1
	srv, iss := newServer(t)
	_, out := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/items", `{"variantId":1,"quantity":2}`)
	id := int(out["items"].([]any)[0].(map[string]any)["cartItemId"].(float64))
	path := "/cart/items/" + itoa(id)
	if s, _ := do(t, srv, iss, 8, "CUSTOMER", "PATCH", path, `{"quantity":1}`); s != 404 {
		t.Errorf("other customer PATCH: got %d, want 404", s)
	}
	if s, _ := do(t, srv, iss, 8, "CUSTOMER", "DELETE", path, ""); s != 404 {
		t.Errorf("other customer DELETE: got %d, want 404", s)
	}
	if s, _ := do(t, srv, iss, 7, "CUSTOMER", "PATCH", path, `{"quantity":1}`); s != 200 {
		t.Errorf("owner PATCH: got %d, want 200", s)
	}
}

func TestPatchValidation(t *testing.T) {
	srv, iss := newServer(t)
	_, out := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/items", `{"variantId":1,"quantity":2}`)
	id := int(out["items"].([]any)[0].(map[string]any)["cartItemId"].(float64))
	path := "/cart/items/" + itoa(id)
	if s, _ := do(t, srv, iss, 7, "CUSTOMER", "PATCH", path, `{}`); s != 400 {
		t.Errorf("omitted quantity: got %d, want 400 (must not default to 0 and delete)", s)
	}
	if s, _ := do(t, srv, iss, 7, "CUSTOMER", "PATCH", path, `{"quantity":-1}`); s != 400 {
		t.Errorf("negative quantity: got %d, want 400", s)
	}
	if s, _ := do(t, srv, iss, 7, "CUSTOMER", "PATCH", "/cart/items/abc", `{"quantity":1}`); s != 400 {
		t.Errorf("non-numeric id: got %d, want 400", s)
	}
	if s, out := do(t, srv, iss, 7, "CUSTOMER", "PATCH", path, `{"quantity":0}`); s != 200 || len(out["items"].([]any)) != 0 {
		t.Errorf("quantity 0: got %d %v, want 200 and an empty cart", s, out)
	}
}

func TestMerge(t *testing.T) {
	srv, iss := newServer(t)
	if s, out := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/merge", `{"items":[]}`); s != 200 || len(out["items"].([]any)) != 0 {
		t.Errorf("empty merge: got %d %v", s, out)
	}
	if s, out := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/merge", `{"items":[{"variantId":1,"quantity":2},{"variantId":404,"quantity":1}]}`); s != 200 || len(out["items"].([]any)) != 1 {
		t.Errorf("merge with a ghost variant: got %d %v, want 200 and just the real line", s, out)
	}
	bad := map[string]string{
		"missing items":         `{}`,
		"quantity 0 in item":    `{"items":[{"variantId":1,"quantity":0}]}`,
		"quantity overflow":     `{"items":[{"variantId":1,"quantity":3000000000}]}`,
		"quantity over max":     `{"items":[{"variantId":1,"quantity":1001}]}`,
		"bad variantId in item": `{"items":[{"variantId":0,"quantity":1}]}`,
		"101 items":             `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"variantId":1,"quantity":1},`, 101), ",") + `]}`,
	}
	for name, body := range bad {
		if s, _ := do(t, srv, iss, 7, "CUSTOMER", "POST", "/cart/merge", body); s != 400 {
			t.Errorf("%s: got %d, want 400", name, s)
		}
	}
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }
