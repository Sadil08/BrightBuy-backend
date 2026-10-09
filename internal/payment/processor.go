package payment

import (
	"context"
	"errors"
)

var (
	ErrCardTokenRequired = errors.New("card token is required")
	ErrIdempotencyKey    = errors.New("payment idempotency key is required")
	ErrAuthorization     = errors.New("payment authorization failed")
)

type PaymentAuth struct {
	ProviderReference string
	LastFour          string
}

type PaymentProcessor interface {
	Authorize(ctx context.Context, amountCents int64, cardToken, idempotencyKey string) (PaymentAuth, error)
	// Void releases an authorization that will never be captured — checkout calls it when the order
	// could not be created after the card was authorized (e.g. stock ran out), so the customer isn't
	// left with money held for an order that doesn't exist (FR-CHECKOUT-14's other half).
	Void(ctx context.Context, providerReference string) error
}
