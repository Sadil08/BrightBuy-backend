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
}
