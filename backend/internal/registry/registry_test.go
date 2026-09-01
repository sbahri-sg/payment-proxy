package registry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/registry"
	"github.com/emisell/api-payment-proxy/internal/xendit"
)

type manifestConnector struct {
	connector.Connector
	code     string
	manifest connector.Manifest
}

func (c manifestConnector) Code() string                 { return c.code }
func (c manifestConnector) Manifest() connector.Manifest { return c.manifest }

func TestRegistryRejectsInvalidAndDuplicateConnectors(t *testing.T) {
	base, err := xendit.New("https://api.xendit.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.New(); err == nil {
		t.Fatal("empty registry was accepted")
	}
	if _, err = registry.New(base, base); err == nil {
		t.Fatal("duplicate connector was accepted")
	}
	invalid := manifestConnector{Connector: base, code: "broken", manifest: connector.Manifest{Code: "broken"}}
	if _, err = registry.New(invalid); err == nil {
		t.Fatal("invalid manifest was accepted")
	}
	mismatch := manifestConnector{Connector: base, code: "alias", manifest: base.Manifest()}
	if _, err = registry.New(mismatch); err == nil {
		t.Fatal("connector and manifest code mismatch was accepted")
	}
}

func TestRegistryProvidesImmutableManifestAndCapabilities(t *testing.T) {
	base, _ := xendit.New("https://api.xendit.test", time.Second)
	items, err := registry.New(base)
	if err != nil {
		t.Fatal(err)
	}
	if codes := items.Codes(); len(codes) != 1 || codes[0] != "xendit" {
		t.Fatalf("unexpected registry codes: %#v", codes)
	}
	supported, err := items.Supports("XENDIT", connector.OperationCreatePayment)
	if err != nil || !supported {
		t.Fatalf("registered operation was not found: %v", err)
	}
	supported, err = items.Supports("xendit", connector.OperationCreateRefund)
	if err != nil || supported {
		t.Fatalf("refund operation was advertised before sandbox certification: %v", err)
	}
	manifest, err := items.Manifest("xendit")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Operations[0] = connector.OperationCreateRefund
	manifest.CertificationProfiles["card"] = connector.CertificationProfile{Code: "mutated"}
	again, _ := items.Manifest("xendit")
	if again.Operations[0] == connector.OperationCreateRefund || again.CertificationProfiles["card"].Code == "mutated" {
		t.Fatal("caller mutated the registered manifest")
	}
	_, err = items.Connector("missing")
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "CONNECTOR_NOT_AVAILABLE" {
		t.Fatalf("unexpected missing connector error: %v", err)
	}
}

func TestRegistryResolvesSharedRuntimeByProviderVersion(t *testing.T) {
	base, _ := xendit.New("https://api.xendit.test", time.Second)
	secondManifest := base.Manifest()
	secondManifest.Version = "emisell-xendit-v2"
	second := manifestConnector{Connector: base, code: "xendit", manifest: secondManifest}

	items, err := registry.New(base, second)
	if err != nil {
		t.Fatalf("versioned runtimes were rejected: %v", err)
	}
	if codes := items.Codes(); len(codes) != 1 || codes[0] != "xendit" {
		t.Fatalf("provider codes must remain unique: %#v", codes)
	}
	manifest, err := items.ManifestVersion("xendit", "emisell-xendit-v2")
	if err != nil || manifest.Version != "emisell-xendit-v2" {
		t.Fatalf("versioned runtime was not resolved: %#v, %v", manifest, err)
	}
	_, err = items.Manifest("xendit")
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "CONNECTOR_VERSION_REQUIRED" {
		t.Fatalf("ambiguous unversioned lookup was accepted: %v", err)
	}
}
