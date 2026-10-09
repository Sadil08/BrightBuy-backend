package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	cartdomain "brightbuy-backend/internal/cart/domain"
	orderdomain "brightbuy-backend/internal/ordering/domain"
	"brightbuy-backend/internal/payment"
)

var (
	ErrEmptyCart      = errors.New("cart is empty")
	ErrInvalidRequest = errors.New("invalid checkout request")
	ErrStockExceeded  = errors.New("one or more items are no longer available")
	ErrPaymentFailed  = errors.New("payment authorization failed")
	ErrOrderNotFound  = errors.New("order not found")
)

type PlaceOrderRequest struct {
	DeliveryMode    orderdomain.DeliveryMode
	DeliveryCityID  *int
	DeliveryAddress string
	PaymentMethod   orderdomain.PaymentMethod
	CardToken       string
}

type CartService interface {
	GetCart(context.Context, int) (*cartdomain.Cart, error)
	ClearItemsInTx(context.Context, *sql.Tx, int) error
}

type OrderRepository interface {
	GetConfig(context.Context, string) (string, error)
	CityExists(context.Context, int) (bool, error)
	FindByIdempotencyKey(context.Context, int, string) (int, error)
	CallPlaceOrder(context.Context, *sql.Tx, PlaceOrderCommand) (int, bool, error)
	GetByID(context.Context, int, int) (*orderdomain.Order, error)
	ListByCustomer(context.Context, int, int, int) ([]orderdomain.Order, int, error)
	DiagnoseUnavailableLines(context.Context, []cartdomain.CartItem) ([]orderdomain.UnavailableLine, error)
}

type PlaceOrderCommand struct {
	CustomerID        int
	ItemsJSON         []byte
	DeliveryMode      orderdomain.DeliveryMode
	DeliveryCityID    *int
	DeliveryAddress   string
	PaymentMethod     orderdomain.PaymentMethod
	IdempotencyKey    string
	TaxAmountCents    int64
	DeliveryFeeCents  int64
	ProviderReference string
	CardLastFour      string
}

type TransactionRunner func(context.Context, func(*sql.Tx) error) error

type CheckoutService struct {
	cart     CartService
	orders   OrderRepository
	payments payment.PaymentProcessor
	withTx   TransactionRunner
}

func NewCheckoutService(cart CartService, orders OrderRepository, payments payment.PaymentProcessor, withTx TransactionRunner) *CheckoutService {
	return &CheckoutService{cart: cart, orders: orders, payments: payments, withTx: withTx}
}

func (s *CheckoutService) PlaceOrder(
	ctx context.Context,
	customerID int,
	idempotencyKey string,
	req PlaceOrderRequest,
) (*orderdomain.Order, error) {
	if customerID <= 0 || !validIdempotencyKey(idempotencyKey) {
		return nil, ErrInvalidRequest
	}
	if err := validateRequest(req); err != nil {
		return nil, err
	}
	if orderID, err := s.orders.FindByIdempotencyKey(ctx, customerID, idempotencyKey); err == nil {
		return s.orders.GetByID(ctx, customerID, orderID)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("find idempotent order: %w", err)
	}

	cart, err := s.cart.GetCart(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("load cart: %w", err)
	}
	if cart == nil || len(cart.Items) == 0 {
		return nil, ErrEmptyCart
	}
	if req.DeliveryMode == orderdomain.StandardDelivery {
		exists, err := s.orders.CityExists(ctx, *req.DeliveryCityID)
		if err != nil {
			return nil, fmt.Errorf("validate delivery city: %w", err)
		}
		if !exists {
			return nil, ErrInvalidRequest
		}
	}

	taxRateRaw, err := s.orders.GetConfig(ctx, "tax_rate_percent")
	if err != nil {
		return nil, fmt.Errorf("load tax configuration: %w", err)
	}
	taxRate, err := parseConfigDecimal(taxRateRaw)
	if err != nil || taxRate < 0 || taxRate > 10000 {
		return nil, fmt.Errorf("invalid tax_rate_percent configuration")
	}
	var deliveryFee int64
	if req.DeliveryMode == orderdomain.StandardDelivery {
		feeRaw, err := s.orders.GetConfig(ctx, "standard_delivery_fee")
		if err != nil {
			return nil, fmt.Errorf("load delivery configuration: %w", err)
		}
		deliveryFee, err = parseConfigDecimal(feeRaw)
		if err != nil || deliveryFee < 0 {
			return nil, fmt.Errorf("invalid standard_delivery_fee configuration")
		}
	}
	taxAmount := (int64(cart.Subtotal)*taxRate + 5000) / 10000
	paymentAmount := int64(cart.Subtotal) + taxAmount + deliveryFee

	var paymentAuth payment.PaymentAuth
	if req.PaymentMethod == orderdomain.PaymentCard {
		providerIdempotencyKey := fmt.Sprintf("customer-%d-%s", customerID, idempotencyKey)
		paymentAuth, err = s.payments.Authorize(ctx, paymentAmount, req.CardToken, providerIdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrPaymentFailed, err)
		}
	}

	itemsJSON, err := cartItemsJSON(cart.Items)
	if err != nil {
		return nil, err
	}
	command := PlaceOrderCommand{
		CustomerID:        customerID,
		ItemsJSON:         itemsJSON,
		DeliveryMode:      req.DeliveryMode,
		DeliveryCityID:    req.DeliveryCityID,
		DeliveryAddress:   strings.TrimSpace(req.DeliveryAddress),
		PaymentMethod:     req.PaymentMethod,
		IdempotencyKey:    idempotencyKey,
		TaxAmountCents:    taxAmount,
		DeliveryFeeCents:  deliveryFee,
		ProviderReference: paymentAuth.ProviderReference,
		CardLastFour:      paymentAuth.LastFour,
	}

	var orderID int
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		createdOrderID, isNew, callErr := s.orders.CallPlaceOrder(ctx, tx, command)
		if callErr != nil {
			return callErr
		}
		orderID = createdOrderID
		// Idempotent replays return the original order without changing the cart.
		if isNew {
			return s.cart.ClearItemsInTx(ctx, tx, customerID)
		}
		return nil
	})
	if err != nil && paymentAuth.ProviderReference != "" {
		// The card was authorized before the transaction, but no order exists: release the hold so
		// the customer isn't charged for nothing. WithoutCancel so a dropped client connection (which
		// cancels ctx) can't skip the release. Best-effort — a failure is logged for reconciliation.
		voidCtx := context.WithoutCancel(ctx)
		if voidErr := s.payments.Void(voidCtx, paymentAuth.ProviderReference); voidErr != nil {
			slog.ErrorContext(voidCtx, "could not void authorization after failed checkout",
				"providerReference", paymentAuth.ProviderReference, "error", voidErr)
		}
	}
	if errors.Is(err, ErrStockExceeded) {
		unavailable, diagnosticErr := s.orders.DiagnoseUnavailableLines(ctx, cart.Items)
		if diagnosticErr != nil {
			return nil, fmt.Errorf("diagnose unavailable order lines: %w", diagnosticErr)
		}
		return nil, &StockExceededError{Lines: unavailable}
	}
	if err != nil {
		return nil, fmt.Errorf("place order transaction: %w", err)
	}
	return s.orders.GetByID(ctx, customerID, orderID)
}

func (s *CheckoutService) GetOrder(ctx context.Context, customerID, orderID int) (*orderdomain.Order, error) {
	return s.orders.GetByID(ctx, customerID, orderID)
}

func (s *CheckoutService) ListOrders(ctx context.Context, customerID, page, size int) ([]orderdomain.Order, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return s.orders.ListByCustomer(ctx, customerID, page, size)
}

type StockExceededError struct {
	Lines []orderdomain.UnavailableLine
}

func (e *StockExceededError) Error() string { return ErrStockExceeded.Error() }
func (e *StockExceededError) Unwrap() error { return ErrStockExceeded }

func validateRequest(req PlaceOrderRequest) error {
	switch req.DeliveryMode {
	case orderdomain.StorePickup:
		if req.DeliveryCityID != nil || strings.TrimSpace(req.DeliveryAddress) != "" {
			return ErrInvalidRequest
		}
	case orderdomain.StandardDelivery:
		if req.DeliveryCityID == nil || *req.DeliveryCityID <= 0 || strings.TrimSpace(req.DeliveryAddress) == "" ||
			len(strings.TrimSpace(req.DeliveryAddress)) > 255 {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}
	switch req.PaymentMethod {
	case orderdomain.PaymentCOD:
		if req.CardToken != "" {
			return ErrInvalidRequest
		}
	case orderdomain.PaymentCard:
		if strings.TrimSpace(req.CardToken) == "" {
			return ErrInvalidRequest
		}
	default:
		return ErrInvalidRequest
	}
	return nil
}

func validIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > 64 || strings.TrimSpace(key) != key {
		return false
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseConfigDecimal(value string) (int64, error) {
	whole, fraction, hasFraction := strings.Cut(value, ".")
	if !hasFraction {
		fraction = ""
	}
	if len(fraction) > 2 || whole == "" {
		return 0, fmt.Errorf("invalid decimal")
	}
	fraction += strings.Repeat("0", 2-len(fraction))
	dollars, err := parseUnsigned(whole)
	if err != nil {
		return 0, err
	}
	cents, err := parseUnsigned(fraction)
	if err != nil {
		return 0, err
	}
	return dollars*100 + cents, nil
}

func parseUnsigned(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid decimal")
		}
	}
	var n int64
	if _, err := fmt.Sscan(value, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func cartItemsJSON(items []cartdomain.CartItem) ([]byte, error) {
	lines := make([]struct {
		VariantID int `json:"variant_id"`
		Quantity  int `json:"quantity"`
	}, 0, len(items))
	for _, item := range items {
		lines = append(lines, struct {
			VariantID int `json:"variant_id"`
			Quantity  int `json:"quantity"`
		}{VariantID: item.VariantID, Quantity: item.Quantity})
	}
	return json.Marshal(lines)
}
