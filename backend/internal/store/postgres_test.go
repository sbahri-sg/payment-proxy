package store

import (
	"testing"

	"github.com/emisell/api-payment-proxy/internal/model"
)

func TestCanonicalPaymentStatus(t *testing.T) {
	tests := []struct {
		current, incoming, expected string
	}{
		{model.PaymentPending, model.PaymentSucceeded, model.PaymentSucceeded},
		{model.PaymentSucceeded, model.PaymentPending, model.PaymentSucceeded},
		{model.PaymentCancelled, model.PaymentPending, model.PaymentCancelled},
		{model.PaymentFailed, model.PaymentSucceeded, model.PaymentSucceeded},
		{model.PaymentUnknown, model.PaymentPending, model.PaymentPending},
		{model.PaymentSucceeded, model.PaymentFailed, model.PaymentSucceeded},
		{model.PaymentSucceeded, model.PaymentExpired, model.PaymentSucceeded},
		{model.PaymentFailed, model.PaymentPending, model.PaymentFailed},
		{model.PaymentCancelled, model.PaymentFailed, model.PaymentCancelled},
		{model.PaymentExpired, model.PaymentProcessing, model.PaymentExpired},
		{model.PaymentCancelled, model.PaymentSucceeded, model.PaymentSucceeded},
		{model.PaymentExpired, model.PaymentSucceeded, model.PaymentSucceeded},
	}
	for _, test := range tests {
		if got := canonicalPaymentStatus(test.current, test.incoming); got != test.expected {
			t.Fatalf("%s -> %s: got %s, want %s", test.current, test.incoming, got, test.expected)
		}
	}
}
