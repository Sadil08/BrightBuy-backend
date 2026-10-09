package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	cartdomain "brightbuy-backend/internal/cart/domain"
	orderdomain "brightbuy-backend/internal/ordering/domain"
	"brightbuy-backend/internal/payment"
)

type fakeCart struct {
	cart    *cartdomain.Cart
	cleared bool
}

func (f *fakeCart) GetCart(context.Context, int) (*cartdomain.Cart, error) { return f.cart, nil }
func (f *fakeCart) ClearItemsInTx(context.Context, *sql.Tx, int) error {
	f.cleared = true
	return nil
}

type fakeOrders struct {
	config      map[string]string
	existingID  int
	command     PlaceOrderCommand
	order       *orderdomain.Order
	unavailable []orderdomain.UnavailableLine
}

func (f *fakeOrders) GetConfig(_ context.Context, key string) (string, error) {
	return f.config[key], nil
}
func (f *fakeOrders) CityExists(context.Context, int) (bool, error) { return true, nil }
func (f *fakeOrders) FindByIdempotencyKey(context.Context, int, string) (int, error) {
	if f.existingID > 0 {
		return f.existingID, nil
	}
	return 0, sql.ErrNoRows
}
func (f *fakeOrders) CallPlaceOrder(_ context.Context, _ *sql.Tx, command PlaceOrderCommand) (int, bool, error) {
	f.command = command
	return 41, true, nil
}
func (f *fakeOrders) GetByID(context.Context, int, int) (*orderdomain.Order, error) {
	return f.order, nil
}
func (f *fakeOrders) ListByCustomer(context.Context, int, int, int) ([]orderdomain.Order, int, error) {
	return nil, 0, nil
}
func (f *fakeOrders) DiagnoseUnavailableLines(context.Context, []cartdomain.CartItem) ([]orderdomain.UnavailableLine, error) {
	return f.unavailable, nil
}

type fakePayments struct {
	auth   payment.PaymentAuth
	err    error
	amount int64
	key    string
	called bool
	voided []string
}

func (f *fakePayments) Void(_ context.Context, providerReference string) error {
	f.voided = append(f.voided, providerReference)
	return nil
}

func (f *fakePayments) Authorize(_ context.Context, amount int64, _, idempotencyKey string) (payment.PaymentAuth, error) {
	f.called = true
	f.amount = amount
	f.key = idempotencyKey
	return f.auth, f.err
}

func TestPlaceOrderCalculatesConfiguredTotalsAndClearsCartAtomically(t *testing.T) {
	cart := &fakeCart{cart: &cartdomain.Cart{
		CustomerID: 8, Subtotal: 10000,
		Items: []cartdomain.CartItem{{VariantID: 12, Quantity: 2}},
	}}
	orders := &fakeOrders{
		config: map[string]string{"tax_rate_percent": "8.25", "standard_delivery_fee": "9.99"},
		order:  &orderdomain.Order{ID: 41},
	}
	payments := &fakePayments{}
	txRan := false
	service := NewCheckoutService(cart, orders, payments, func(_ context.Context, fn func(*sql.Tx) error) error {
		txRan = true
		return fn(nil)
	})

	got, err := service.PlaceOrder(context.Background(), 8, "attempt-1", PlaceOrderRequest{
		DeliveryMode: orderdomain.StandardDelivery, DeliveryCityID: intPtr(2),
		DeliveryAddress: "1 Main St", PaymentMethod: orderdomain.PaymentCOD,
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if got.ID != 41 || !txRan || !cart.cleared {
		t.Fatalf("order=%+v txRan=%v cartCleared=%v", got, txRan, cart.cleared)
	}
	if orders.command.TaxAmountCents != 825 || orders.command.DeliveryFeeCents != 999 {
		t.Fatalf("tax/fee = %d/%d cents, want 825/999", orders.command.TaxAmountCents, orders.command.DeliveryFeeCents)
	}
	if payments.called {
		t.Fatal("COD checkout unexpectedly called payment processor")
	}
}

func TestPlaceOrderAuthorizesCardBeforeOpeningTransaction(t *testing.T) {
	cart := &fakeCart{cart: &cartdomain.Cart{CustomerID: 8, Subtotal: 10000, Items: []cartdomain.CartItem{{VariantID: 12, Quantity: 1}}}}
	orders := &fakeOrders{config: map[string]string{"tax_rate_percent": "8.25"}}
	payments := &fakePayments{auth: payment.PaymentAuth{ProviderReference: "ref-1", LastFour: "4242"}}
	txSawAuthorization := false
	service := NewCheckoutService(cart, orders, payments, func(_ context.Context, fn func(*sql.Tx) error) error {
		txSawAuthorization = payments.called
		return fn(nil)
	})
	orders.order = &orderdomain.Order{ID: 42}

	_, err := service.PlaceOrder(context.Background(), 8, "attempt-card", PlaceOrderRequest{
		DeliveryMode: orderdomain.StorePickup, PaymentMethod: orderdomain.PaymentCard, CardToken: "tok_test",
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if !txSawAuthorization || payments.amount != 10825 || payments.key != "customer-8-attempt-card" {
		t.Fatalf("authorization ordering/amount/key invalid: txSawAuthorization=%v amount=%d key=%q", txSawAuthorization, payments.amount, payments.key)
	}
	if orders.command.ProviderReference != "ref-1" || orders.command.CardLastFour != "4242" {
		t.Fatalf("payment reference not passed to repository: %+v", orders.command)
	}
}

func TestPlaceOrderAuthorizationFailureDoesNotStartTransaction(t *testing.T) {
	cart := &fakeCart{cart: &cartdomain.Cart{CustomerID: 8, Subtotal: 10000, Items: []cartdomain.CartItem{{VariantID: 12, Quantity: 1}}}}
	orders := &fakeOrders{config: map[string]string{"tax_rate_percent": "8.25"}}
	payments := &fakePayments{err: payment.ErrAuthorization}
	txRan := false
	service := NewCheckoutService(cart, orders, payments, func(context.Context, func(*sql.Tx) error) error {
		txRan = true
		return nil
	})
	_, err := service.PlaceOrder(context.Background(), 8, "attempt-fail", PlaceOrderRequest{
		DeliveryMode: orderdomain.StorePickup, PaymentMethod: orderdomain.PaymentCard, CardToken: "tok_decline",
	})
	if !errors.Is(err, ErrPaymentFailed) {
		t.Fatalf("error=%v, want ErrPaymentFailed", err)
	}
	if txRan || cart.cleared {
		t.Fatalf("failed authorization changed state: txRan=%v cartCleared=%v", txRan, cart.cleared)
	}
}

func TestPlaceOrderVoidsCardAuthorizationWhenOrderCannotBeCreated(t *testing.T) {
	cart := &fakeCart{cart: &cartdomain.Cart{CustomerID: 8, Subtotal: 10000, Items: []cartdomain.CartItem{{VariantID: 12, Quantity: 1}}}}
	orders := &fakeOrders{config: map[string]string{"tax_rate_percent": "8.25"}}
	payments := &fakePayments{auth: payment.PaymentAuth{ProviderReference: "ref-9", LastFour: "4242"}}
	service := NewCheckoutService(cart, orders, payments, func(context.Context, func(*sql.Tx) error) error {
		return ErrStockExceeded // as sp_place_order reports when stock ran out
	})
	_, err := service.PlaceOrder(context.Background(), 8, "attempt-stock", PlaceOrderRequest{
		DeliveryMode: orderdomain.StorePickup, PaymentMethod: orderdomain.PaymentCard, CardToken: "tok_test",
	})
	if !errors.Is(err, ErrStockExceeded) {
		t.Fatalf("error=%v, want ErrStockExceeded", err)
	}
	if len(payments.voided) != 1 || payments.voided[0] != "ref-9" {
		t.Fatalf("voided=%v, want the authorization released exactly once", payments.voided)
	}
	if cart.cleared {
		t.Fatal("cart must stay intact when the order fails (AC-CHECKOUT-4)")
	}
}

func TestPlaceOrderDoesNotVoidWhenNoCardWasAuthorized(t *testing.T) {
	cart := &fakeCart{cart: &cartdomain.Cart{CustomerID: 8, Subtotal: 10000, Items: []cartdomain.CartItem{{VariantID: 12, Quantity: 1}}}}
	orders := &fakeOrders{config: map[string]string{"tax_rate_percent": "8.25"}}
	payments := &fakePayments{}
	service := NewCheckoutService(cart, orders, payments, func(context.Context, func(*sql.Tx) error) error {
		return ErrStockExceeded
	})
	_, _ = service.PlaceOrder(context.Background(), 8, "attempt-cod", PlaceOrderRequest{
		DeliveryMode: orderdomain.StorePickup, PaymentMethod: orderdomain.PaymentCOD,
	})
	if len(payments.voided) != 0 {
		t.Fatalf("voided=%v, want none for COD", payments.voided)
	}
}

func intPtr(value int) *int { return &value }
