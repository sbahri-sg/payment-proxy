package engine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/engine"
	"github.com/emisell/api-payment-proxy/internal/registry"
	"github.com/emisell/api-payment-proxy/internal/xendit"
)

func TestRuntimeRoutesMetadataAndValidationThroughRegistry(t *testing.T) {
	client, _ := xendit.New("https://api.xendit.test", time.Second)
	items, err := registry.New(client)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := engine.New(items)
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	manifest, err := runtime.Manifest("xendit")
	if err != nil || manifest.Version != "emisell-xendit-v1" {
		t.Fatalf("unexpected manifest: %#v, %v", manifest, err)
	}
	if err = runtime.ValidatePaymentMethod("xendit", connector.PaymentMethodMapping{
		PaymentMethodCode: "qris", ProviderMethod: "qr_code", ProviderMethodType: "qris",
	}); err != nil {
		t.Fatalf("valid method mapping was rejected: %v", err)
	}
	if err = runtime.ValidatePayment("xendit", connector.PaymentValidation{PaymentMethodCode: "qris", Currency: "IDR", Amount: 1_000_000}); err != nil {
		t.Fatalf("valid payment was rejected: %v", err)
	}
	supported, err := runtime.Supports("xendit", connector.OperationCreateRefund)
	if err != nil || supported {
		t.Fatalf("runtime advertised unsupported refund: %v", err)
	}
}

func TestRuntimeRejectsInvalidPaymentBeforeProviderIO(t *testing.T) {
	client, _ := xendit.New("https://api.xendit.test", time.Second)
	items, _ := registry.New(client)
	runtime, _ := engine.New(items)
	_, err := runtime.CreatePayment(context.Background(), connector.PaymentInput{
		ProviderCode: "xendit", PaymentMethodCode: "qris", Currency: "IDR", Amount: 1_000_001,
	})
	if err == nil || errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("invalid payment reached provider execution: %v", err)
	}
	_, err = runtime.Manifest("missing")
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "CONNECTOR_NOT_AVAILABLE" {
		t.Fatalf("unexpected missing connector error: %v", err)
	}
}
