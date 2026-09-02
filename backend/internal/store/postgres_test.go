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

func TestNormalizePaymentSelection(t *testing.T) {
	hosted := model.PaymentSession{PaymentMethodID: "pmo_qris", PaymentMethodCode: "qris"}
	normalizePaymentSelection(&hosted)
	if hosted.CheckoutMode != "provider_hosted" || hosted.PaymentMethodID != "pmo_qris" || hosted.PaymentOptionID != "" || hosted.PaymentMethodCode != "qris" {
		t.Fatalf("unexpected hosted payment selection: %#v", hosted)
	}

	direct := model.PaymentSession{PaymentOptionID: "pmo_card", PaymentMethodCode: "card"}
	normalizePaymentSelection(&direct)
	if direct.CheckoutMode != "direct" || direct.PaymentOptionID != "pmo_card" || direct.PaymentMethodID != "" {
		t.Fatalf("unexpected direct payment selection: %#v", direct)
	}
}

func TestCanonicalPaymentFlags(t *testing.T) {
	tests := []struct {
		name                    string
		existing                []string
		previous, applied       string
		expected, expectedAdded []string
	}{
		{name: "late payment", previous: model.PaymentExpired, applied: model.PaymentSucceeded, expected: []string{"late_payment"}, expectedAdded: []string{"late_payment"}},
		{name: "delayed confirmation", previous: model.PaymentUnknown, applied: model.PaymentSucceeded, expected: []string{"provider_delayed_confirmation"}, expectedAdded: []string{"provider_delayed_confirmation"}},
		{name: "ordinary success", previous: model.PaymentPending, applied: model.PaymentSucceeded, expected: []string{}, expectedAdded: []string{}},
		{name: "preserve and deduplicate", existing: []string{"late_payment"}, previous: model.PaymentExpired, applied: model.PaymentSucceeded, expected: []string{"late_payment"}, expectedAdded: []string{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flags, added := canonicalPaymentFlags(test.existing, test.previous, test.applied)
			if !sameStrings(flags, test.expected) {
				t.Fatalf("flags = %#v, want %#v", flags, test.expected)
			}
			if !sameStrings(added, test.expectedAdded) {
				t.Fatalf("added = %#v, want %#v", added, test.expectedAdded)
			}
		})
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestValidRuntimeDigest(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, test := range []struct {
		value string
		valid bool
	}{
		{value: valid, valid: true},
		{value: "", valid: false},
		{value: valid[:63], valid: false},
		{value: "G" + valid[1:], valid: false},
		{value: "A" + valid[1:], valid: false},
	} {
		if got := validRuntimeDigest(test.value); got != test.valid {
			t.Fatalf("validRuntimeDigest(%q) = %v, want %v", test.value, got, test.valid)
		}
	}
}
