package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/cart/domain"
	catalogapp "brightbuy-backend/internal/catalog/app"
	"brightbuy-backend/internal/shared/money"
)

// fakeStore is an in-memory Store: lines[customerID][variantID] = quantity, with cart item ids
// handed out sequentially. stock maps variantID -> available; a variant missing from it is "not found".
type fakeStore struct {
	lines  map[int]map[int]int
	ids    map[int]map[int]int // customerID -> variantID -> cartItemID
	nextID int
}

func newFake() *fakeStore {
	return &fakeStore{
		lines:  map[int]map[int]int{},
		ids:    map[int]map[int]int{},
		nextID: 1,
	}
}

func (f *fakeStore) Get(_ context.Context, c int) (*domain.Cart, error) {
	cart := &domain.Cart{CustomerID: c, Items: []domain.CartItem{}}
	for v, q := range f.lines[c] {
		cart.Items = append(cart.Items, domain.CartItem{ID: f.ids[c][v], VariantID: v, Quantity: q})
	}
	return cart, nil
}
func (f *fakeStore) UpsertLine(ctx context.Context, c, v, q int) (*domain.Cart, error) {
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
func (f *fakeStore) FindLine(_ context.Context, c, v int) (*domain.CartItem, error) {
	q, ok := f.lines[c][v]
	if !ok {
		return nil, nil
	}
	return &domain.CartItem{ID: f.ids[c][v], VariantID: v, Quantity: q}, nil
}
func (f *fakeStore) FindLineByID(_ context.Context, c, id int) (*domain.CartItem, error) {
	for v, lid := range f.ids[c] { // only this customer's lines are searched: ownership scoping
		if lid == id {
			return &domain.CartItem{ID: id, VariantID: v, Quantity: f.lines[c][v]}, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) DeleteLine(_ context.Context, c, v int) error {
	delete(f.lines[c], v)
	delete(f.ids[c], v)
	return nil
}
func (f *fakeStore) CustomerIDForUser(context.Context, int) (int, error) { return 0, nil }

func newTestService(stock map[int]int) (*Service, *fakeStore) {
	store := newFake()
	catalog := fakeCatalog{stock: stock}
	return NewService(store, catalog), store
}

var ctx = context.Background()

func TestAddItemAccumulatesQuantityForExistingVariant(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 10})
	_, _ = s.AddItem(ctx, 1, 1, 2)
	cart, err := s.AddItem(ctx, 1, 1, 3)
	if err != nil || cart.Items[0].Quantity != 5 {
		t.Fatalf("got %+v, %v; want quantity 5 (2 + 3)", cart, err)
	}
}

func TestAddItemRejectsAccumulatedQuantityOverStock(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 4})
	if _, err := s.AddItem(ctx, 1, 1, 2); err != nil {
		t.Fatalf("first AddItem: %v", err)
	}
	if _, err := s.AddItem(ctx, 1, 1, 3); err == nil {
		t.Fatal("want stock error when accumulated quantity exceeds available stock")
	}
}

func TestAddItemRejectsOverStockWithAvailableCount(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 3})
	_, err := s.AddItem(ctx, 1, 1, 4) // AC-CART-2
	var se *domain.StockExceededError
	if !errors.As(err, &se) || se.Available != 3 {
		t.Fatalf("got %v, want StockExceededError{Available:3}", err)
	}
}

func TestAddItemUnknownVariant(t *testing.T) {
	s, _ := newTestService(nil)
	if _, err := s.AddItem(ctx, 1, 99, 1); !errors.Is(err, domain.ErrVariantNotFound) {
		t.Fatalf("got %v, want ErrVariantNotFound", err)
	}
}

func TestGetCartEnrichesItemsAndExcludesUnavailableSubtotal(t *testing.T) {
	store := newFake()
	store.lines[1] = map[int]int{1: 2, 2: 1}
	store.ids[1] = map[int]int{1: 10, 2: 20}
	catalog := fakeCatalog{variants: map[int]catalogapp.CartVariant{
		1: {
			ID: 1, ProductName: "Available product", Price: money.FromCents(1250),
			StockQuantity: 1, Available: true,
		},
		2: {
			ID: 2, ProductName: "Inactive product", Price: money.FromCents(9900),
			StockQuantity: 10, Available: false,
		},
	}}
	service := NewService(store, catalog)

	cart, err := service.GetCart(ctx, 1)
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if len(cart.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(cart.Items))
	}

	items := make(map[int]domain.CartItem, len(cart.Items))
	for _, item := range cart.Items {
		items[item.VariantID] = item
	}
	available := items[1]
	if available.ProductName != "Available product" || available.UnitPrice != 1250 {
		t.Errorf("available item = %+v, want Catalog name and price", available)
	}
	if !available.StockWarning {
		t.Errorf("available item StockWarning = false, want true for quantity 2 with stock 1")
	}
	inactive := items[2]
	if !inactive.Unavailable {
		t.Errorf("inactive item = %+v, want Unavailable true", inactive)
	}
	if cart.Subtotal != 2500 {
		t.Errorf("Subtotal = %d, want 2500 (excluding unavailable line)", cart.Subtotal)
	}
}

func TestInactiveVariantsCannotBeAddedOrMerged(t *testing.T) {
	catalog := fakeCatalog{variants: map[int]catalogapp.CartVariant{
		2: {ID: 2, Available: false},
	}}
	store := newFake()
	service := NewService(store, catalog)

	if _, err := service.AddItem(ctx, 1, 2, 1); !errors.Is(err, domain.ErrVariantNotFound) {
		t.Errorf("AddItem error = %v, want ErrVariantNotFound", err)
	}
	cart, err := service.Merge(ctx, 1, []LineInput{{VariantID: 2, Quantity: 1}})
	if err != nil {
		t.Fatalf("Merge inactive variant: %v", err)
	}
	if len(cart.Items) != 0 {
		t.Errorf("Merge added inactive variant: items = %+v", cart.Items)
	}
}

func TestUpdateAndRemoveAreOwnershipScoped(t *testing.T) { // SEC-CART-1
	s, f := newTestService(map[int]int{1: 10})
	a, _ := s.AddItem(ctx, 1, 1, 2)
	lineID := a.Items[0].ID
	if _, err := s.UpdateItemQuantity(ctx, 2, lineID, 1); !errors.Is(err, domain.ErrLineNotFound) {
		t.Errorf("update by another customer: got %v, want ErrLineNotFound", err)
	}
	if _, err := s.RemoveItem(ctx, 2, lineID); !errors.Is(err, domain.ErrLineNotFound) {
		t.Errorf("remove by another customer: got %v, want ErrLineNotFound", err)
	}
	if f.lines[1][1] != 2 {
		t.Error("owner's line was modified by another customer's request")
	}
}

func TestUpdateQuantityZeroRemovesLine(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 10})
	a, _ := s.AddItem(ctx, 1, 1, 2)
	cart, err := s.UpdateItemQuantity(ctx, 1, a.Items[0].ID, 0)
	if err != nil || len(cart.Items) != 0 {
		t.Fatalf("got %+v, %v; want empty cart", cart, err)
	}
}

func TestUpdateRejectsOverStock(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 3})
	a, _ := s.AddItem(ctx, 1, 1, 1)
	if _, err := s.UpdateItemQuantity(ctx, 1, a.Items[0].ID, 4); err == nil {
		t.Fatal("want stock error")
	}
}

func TestMergeKeepsHigherQuantityNotSum(t *testing.T) { // AC-CART-4
	s, _ := newTestService(map[int]int{1: 10, 2: 10})
	_, _ = s.AddItem(ctx, 1, 1, 1)                            // server has 1 of V1
	cart, err := s.Merge(ctx, 1, []LineInput{{1, 2}, {2, 5}}) // guest has 2 of V1, 5 of V2
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]int{}
	for _, it := range cart.Items {
		got[it.VariantID] = it.Quantity
	}
	if got[1] != 2 || got[2] != 5 {
		t.Fatalf("got %v, want V1=2 (higher, not 3) and V2=5", got)
	}
}

func TestMergeKeepsServerQuantityWhenHigher(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 10})
	_, _ = s.AddItem(ctx, 1, 1, 5)
	cart, _ := s.Merge(ctx, 1, []LineInput{{1, 2}})
	if cart.Items[0].Quantity != 5 {
		t.Fatalf("got %d, want 5", cart.Items[0].Quantity)
	}
}

func TestMergeSkipsUnknownVariantsAndEmptyIsNoop(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 10})
	_, _ = s.AddItem(ctx, 1, 1, 2)
	cart, err := s.Merge(ctx, 1, []LineInput{{99, 1}})
	if err != nil || len(cart.Items) != 1 {
		t.Fatalf("unknown variant: got %+v, %v", cart, err)
	}
	cart, err = s.Merge(ctx, 1, nil)
	if err != nil || len(cart.Items) != 1 || cart.Items[0].Quantity != 2 {
		t.Fatalf("empty merge: got %+v, %v; want existing cart unchanged", cart, err)
	}
}

func TestMoneyMarshalsAsDecimalString(t *testing.T) {
	b, _ := domain.Money(1050).MarshalJSON()
	if string(b) != `"10.50"` {
		t.Fatalf("got %s", b)
	}
}

func TestInvalidQuantitiesAreInvalidInputNotInternalErrors(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 10})
	a, _ := s.AddItem(ctx, 1, 1, 1)
	checks := map[string]error{}
	_, checks["add 0"] = s.AddItem(ctx, 1, 1, 0)
	_, checks["add over max"] = s.AddItem(ctx, 1, 1, domain.MaxLineQuantity+1)
	_, checks["update negative"] = s.UpdateItemQuantity(ctx, 1, a.Items[0].ID, -1)
	_, checks["merge over max"] = s.Merge(ctx, 1, []LineInput{{1, domain.MaxLineQuantity + 1}})
	_, checks["merge too many"] = s.Merge(ctx, 1, make([]LineInput, maxMergeItems+1))
	for name, err := range checks {
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Errorf("%s: got %v, want ErrInvalidInput", name, err)
		}
	}
}

type fakeCatalog struct {
	stock    map[int]int
	variants map[int]catalogapp.CartVariant
}

func (f fakeCatalog) GetVariantForCart(
	_ context.Context,
	variantID int,
) (*catalogapp.CartVariant, error) {
	if variant, ok := f.variants[variantID]; ok {
		return &variant, nil
	}
	stock, ok := f.stock[variantID]
	if !ok {
		return nil, catalogapp.ErrNotFound
	}

	return &catalogapp.CartVariant{
		ID:            variantID,
		ProductName:   "Test product",
		Price:         money.FromCents(100),
		StockQuantity: stock,
		Available:     true,
	}, nil
}
