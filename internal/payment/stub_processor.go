package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

type StubProcessor struct{}

func (StubProcessor) Authorize(ctx context.Context, amountCents int64, cardToken, idempotencyKey string) (PaymentAuth, error) {
	if err := ctx.Err(); err != nil {
		return PaymentAuth{}, err
	}
	if idempotencyKey == "" {
		return PaymentAuth{}, ErrIdempotencyKey
	}
	if amountCents < 0 || len(cardToken) <= len("tok_") || cardToken[:len("tok_")] != "tok_" {
		return PaymentAuth{}, ErrCardTokenRequired
	}
	if cardToken == "tok_decline" {
		return PaymentAuth{}, ErrAuthorization
	}

	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return PaymentAuth{}, fmt.Errorf("generate payment reference: %w", err)
	}
	return PaymentAuth{
		ProviderReference: "stub-" + hex.EncodeToString(random),
		LastFour:          "4242",
	}, nil
}
