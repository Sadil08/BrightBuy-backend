package payment

import (
	"context"
	"errors"
	"testing"
)

func TestStubProcessorAuthorize(t *testing.T) {
	auth, err := (StubProcessor{}).Authorize(context.Background(), 1234, "tok_test", "customer-8-checkout-1")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if auth.ProviderReference == "" || auth.LastFour != "4242" {
		t.Fatalf("unexpected authorization result: %+v", auth)
	}
}

func TestStubProcessorRejectsMissingTokenAndDeclines(t *testing.T) {
	processor := StubProcessor{}
	if _, err := processor.Authorize(context.Background(), 100, "4111111111111111", "checkout-1"); !errors.Is(err, ErrCardTokenRequired) {
		t.Fatalf("raw card number error = %v, want ErrCardTokenRequired", err)
	}
	if _, err := processor.Authorize(context.Background(), 100, "tok_decline", "checkout-1"); !errors.Is(err, ErrAuthorization) {
		t.Fatalf("decline error = %v, want ErrAuthorization", err)
	}
}
