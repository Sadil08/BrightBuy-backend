package app

import (
	"context"
	"errors"
	"testing"

	"brightbuy-backend/internal/cart/domain"
	catalogapp "brightbuy-backend/internal/catalog/app"
)

// fakeStore is an in-memory Store: lines[customerID][variantID] = quantity, with cart item ids
// handed out sequentially. stock maps variantID -> available; a variant missing from it is "not found".
type fakeStore struct {
	lines  map[int]map[int]int
	ids    map[int]map[int]int // customerID -> variantID -> cartItemID
	nextID int
	stock  map[int]int
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
func (f *fakeStore) VariantStock(_ context.Context, v int) (int, error) {
	s, ok := f.stock[v]
	if !ok {
		return 0, domain.ErrVariantNotFound
	}
	return s, nil
}
func (f *fakeStore) CustomerIDForUser(context.Context, int) (int, error) { return 0, nil }

func newTestService(stock map[int]int) (*Service, *fakeStore) {
	store := newFake()
	catalog := fakeCatalog{stock: stock}
	return NewService(store, catalog), store
}

var ctx = context.Background()

func TestAddItemSetsQuantityInsteadOfSumming(t *testing.T) {
	s, _ := newTestService(map[int]int{1: 10})
	_, _ = s.AddItem(ctx, 1, 1, 2)
	cart, err := s.AddItem(ctx, 1, 1, 2)
	if err != nil || cart.Items[0].Quantity != 2 {
		t.Fatalf("got %+v, %v; want quantity 2 (set, not 4)", cart, err)
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
	stock map[int]int
}

func (f fakeCatalog) GetVariantForCart(
	_ context.Context,
	variantID int,
) (*catalogapp.CartVariant, error) {
	stock, ok := f.stock[variantID]
	if !ok {
		return nil, catalogapp.ErrNotFound
	}

	return &catalogapp.CartVariant{
		ID:            variantID,
		StockQuantity: stock,
	}, nil
}
