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

type versionedConnector struct {
	connector.Connector
	version  string
	resultID string
}

func (c versionedConnector) Code() string { return "shared" }
func (c versionedConnector) Manifest() connector.Manifest {
	return connector.Manifest{
		Code: "shared", Name: "Shared", Version: c.version, Runtime: "isolated_container",
		Operations: []connector.Operation{connector.OperationCreatePayment},
	}
}
func (c versionedConnector) ValidatePayment(connector.PaymentValidation) error { return nil }
func (c versionedConnector) CreatePayment(context.Context, connector.PaymentInput) (connector.PaymentResult, error) {
	return connector.PaymentResult{ID: c.resultID, Status: "pending"}, nil
}

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
		t.Fatalf("runtime advertised refund before sandbox certification: %v", err)
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

func TestRuntimeDispatchesMultipleVersionsOfOneProvider(t *testing.T) {
	items, err := registry.New(
		versionedConnector{version: "v1", resultID: "payment-from-v1"},
		versionedConnector{version: "v2", resultID: "payment-from-v2"},
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := engine.New(items)

	for version, expectedID := range map[string]string{"v1": "payment-from-v1", "v2": "payment-from-v2"} {
		result, createErr := runtime.CreatePayment(context.Background(), connector.PaymentInput{
			ProviderCode: "shared", ProviderVersion: version, PaymentMethodCode: "qris", Currency: "IDR", Amount: 1,
		})
		if createErr != nil || result.ID != expectedID {
			t.Fatalf("runtime %s routed to %#v: %v", version, result, createErr)
		}
	}

	_, err = runtime.CreatePayment(context.Background(), connector.PaymentInput{
		ProviderCode: "shared", PaymentMethodCode: "qris", Currency: "IDR", Amount: 1,
	})
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "CONNECTOR_VERSION_REQUIRED" {
		t.Fatalf("ambiguous payment dispatch was accepted: %v", err)
	}
}
