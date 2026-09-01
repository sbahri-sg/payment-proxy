package remoteconnector_test

import (
	"context"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/connectorrunner"
	"github.com/emisell/api-payment-proxy/internal/engine"
	"github.com/emisell/api-payment-proxy/internal/registry"
	"github.com/emisell/api-payment-proxy/internal/remoteconnector"
)

type stubConnector struct{ connector.Connector }

func (stubConnector) Code() string { return "stub" }

func (stubConnector) Manifest() connector.Manifest {
	return connector.Manifest{
		Code: "stub", Name: "Stub", Version: "1.0.0", Runtime: "isolated_container",
		Operations: []connector.Operation{connector.OperationVerifyInstallation, connector.OperationCreatePayment, connector.OperationCreateHostedCheckout},
		CertificationProfiles: map[string]connector.CertificationProfile{
			"qris": {Code: "stub/qris", Automated: true},
		},
	}
}

func (stubConnector) VerifyInstallation(_ context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	return connector.InstallationResult{
		ConnectorID:       "stub:" + input.InstallationID,
		Environment:       input.Environment,
		StoredCredentials: input.Credentials,
		WebhookReady:      true,
		PaymentMethodAvailability: []connector.PaymentMethodAvailability{
			{PaymentMethodCode: "qris", Status: connector.PaymentMethodAvailabilityUnavailable, Reason: "PROVIDER_MAINTENANCE"},
		},
	}, nil
}

func (stubConnector) ValidatePaymentMethod(input connector.PaymentMethodMapping) error {
	if input.PaymentMethodCode != "qris" {
		return connector.ErrNotSupported
	}
	return nil
}

func (stubConnector) ValidateHostedPaymentMethods(input []connector.PaymentMethodMapping) error {
	if len(input) != 1 || input[0].PaymentMethodCode != "qris" {
		return connector.ErrHostedPaymentRestrictionUnsupported
	}
	return nil
}

func (stubConnector) ValidatePayment(input connector.PaymentValidation) error {
	if input.Amount <= 0 {
		return errors.New("amount must be positive")
	}
	return nil
}

func (stubConnector) CreatePayment(_ context.Context, input connector.PaymentInput) (connector.PaymentResult, error) {
	if input.Amount == 999 {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: errors.New("connection lost")}
	}
	return connector.PaymentResult{ID: "pay_remote", Status: "pending"}, nil
}

type versionedStubConnector struct {
	stubConnector
	version string
}

func (c versionedStubConnector) Manifest() connector.Manifest {
	manifest := c.stubConnector.Manifest()
	manifest.Version = c.version
	return manifest
}

func TestDiscoverAndExecuteThroughAuthenticatedRunner(t *testing.T) {
	items, err := registry.New(stubConnector{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := engine.New(items)
	handler, err := connectorrunner.New(runtime, "runner-secret", slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	connectors, err := remoteconnector.Discover(context.Background(), server.URL, "runner-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(connectors) != 1 || connectors[0].Manifest().Runtime != "isolated_container" {
		t.Fatalf("unexpected discovered connectors: %#v", connectors)
	}
	if err = connectors[0].ValidatePaymentMethod(connector.PaymentMethodMapping{PaymentMethodCode: "qris"}); err != nil {
		t.Fatalf("remote validation failed: %v", err)
	}
	if err = connectors[0].ValidateHostedPaymentMethods([]connector.PaymentMethodMapping{{PaymentMethodCode: "qris"}}); err != nil {
		t.Fatalf("remote hosted allowlist validation failed: %v", err)
	}
	if err = connectors[0].ValidateHostedPaymentMethods([]connector.PaymentMethodMapping{{PaymentMethodCode: "qris"}, {PaymentMethodCode: "va_bca"}}); !errors.Is(err, connector.ErrHostedPaymentRestrictionUnsupported) {
		t.Fatalf("remote hosted allowlist error was not preserved: %v", err)
	}
	installationResult, err := connectors[0].VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_remote", ProviderCode: "stub", ProviderVersion: "1.0.0",
		Environment: "sandbox", Credentials: map[string]string{"api_key": "secret"},
	})
	if err != nil || installationResult.StoredCredentials["api_key"] != "secret" || len(installationResult.PaymentMethodAvailability) != 1 || installationResult.PaymentMethodAvailability[0].Reason != "PROVIDER_MAINTENANCE" {
		t.Fatalf("remote availability evidence was not preserved: %#v, %v", installationResult, err)
	}
	result, err := connectors[0].CreatePayment(context.Background(), connector.PaymentInput{
		ProviderCode: "stub", PaymentMethodCode: "qris", Currency: "IDR", Amount: 1000,
	})
	if err != nil || result.ID != "pay_remote" {
		t.Fatalf("unexpected remote payment: %#v, %v", result, err)
	}
	_, err = connectors[0].CreatePayment(context.Background(), connector.PaymentInput{
		ProviderCode: "stub", PaymentMethodCode: "qris", Currency: "IDR", Amount: 999,
	})
	if !errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("unknown outcome was not preserved: %v", err)
	}
}

func TestDiscoveryRejectsInvalidRunnerToken(t *testing.T) {
	items, _ := registry.New(stubConnector{})
	runtime, _ := engine.New(items)
	handler, _ := connectorrunner.New(runtime, "runner-secret", slog.Default())
	server := httptest.NewServer(handler)
	defer server.Close()
	if _, err := remoteconnector.Discover(context.Background(), server.URL, "wrong-secret", time.Second); err == nil {
		t.Fatal("connector discovery accepted an invalid token")
	}
}

func TestDiscoverWithPrivateConnectorCA(t *testing.T) {
	items, _ := registry.New(stubConnector{})
	runtime, _ := engine.New(items)
	handler, _ := connectorrunner.New(runtime, "runner-secret", slog.Default())
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	if _, err := remoteconnector.Discover(context.Background(), server.URL, "runner-secret", time.Second); err == nil {
		t.Fatal("connector discovery trusted an unknown private CA")
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	connectors, err := remoteconnector.DiscoverWithCAPEM(context.Background(), server.URL, "runner-secret", time.Second, caPEM)
	if err != nil {
		t.Fatalf("connector discovery rejected the configured private CA: %v", err)
	}
	if len(connectors) != 1 || connectors[0].Code() != "stub" {
		t.Fatalf("unexpected discovered connectors: %#v", connectors)
	}
}

func TestDiscoverAcceptsMultipleVersionsOfOneProvider(t *testing.T) {
	items, err := registry.New(
		versionedStubConnector{version: "1.0.0"},
		versionedStubConnector{version: "2.0.0"},
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime, _ := engine.New(items)
	handler, _ := connectorrunner.New(runtime, "runner-secret", slog.Default())
	server := httptest.NewServer(handler)
	defer server.Close()

	connectors, err := remoteconnector.Discover(context.Background(), server.URL, "runner-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(connectors) != 2 || connectors[0].Code() != "stub" || connectors[1].Code() != "stub" {
		t.Fatalf("unexpected versioned discovery result: %#v", connectors)
	}
	if _, err = registry.New(connectors...); err != nil {
		t.Fatalf("discovered versions could not be registered together: %v", err)
	}
}
