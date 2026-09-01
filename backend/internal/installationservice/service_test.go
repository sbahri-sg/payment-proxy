package installationservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/emisell/api-payment-proxy/internal/store"
)

type repositoryFake struct {
	provider        model.Provider
	installation    model.Installation
	releasedVersion string
	ciphertext      []byte
	completed       *store.CompleteInstallationVerificationInput
	failed          *store.FailInstallationVerificationInput
	refundLiability bool
	credentialsGone bool
}

func (f *repositoryFake) GetProvider(context.Context, string) (model.Provider, error) {
	return f.provider, nil
}
func (f *repositoryFake) GetReleasedProviderVersion(context.Context, string) (string, error) {
	return f.releasedVersion, nil
}
func (f *repositoryFake) CreateInstallation(_ context.Context, in store.CreateInstallationInput) (model.Installation, error) {
	f.installation = model.Installation{
		ID: in.ID, TenantID: in.TenantID, ProviderCode: in.ProviderCode,
		ProviderVersion: in.ProviderVersion, Environment: in.Environment,
		Status: model.InstallationConfigRequired, Version: 1,
	}
	return f.installation, nil
}
func (f *repositoryFake) GetInstallation(context.Context, string, string) (model.Installation, error) {
	return f.installation, nil
}
func (f *repositoryFake) BeginCredentialConfig(context.Context, string, string, string, string) (model.Installation, error) {
	f.installation.Status = model.InstallationVerifying
	f.installation.Version++
	return f.installation, nil
}
func (f *repositoryFake) GetProviderCredentials(context.Context, string, string) ([]byte, error) {
	if len(f.ciphertext) == 0 {
		return nil, store.ErrNotFound
	}
	return f.ciphertext, nil
}
func (f *repositoryFake) SaveProviderCredentials(_ context.Context, _, _ string, ciphertext []byte) error {
	f.ciphertext = append([]byte(nil), ciphertext...)
	return nil
}
func (f *repositoryFake) CompleteInstallationVerification(_ context.Context, in store.CompleteInstallationVerificationInput) (model.Installation, error) {
	f.completed = &in
	f.installation.Status = model.InstallationReady
	f.installation.EngineConnectorID = in.ConnectorID
	f.installation.CredentialMetadata = in.Metadata
	f.installation.PaymentMethods = in.PaymentMethods
	f.installation.Version++
	return f.installation, nil
}
func (f *repositoryFake) FailInstallationVerification(_ context.Context, in store.FailInstallationVerificationInput) error {
	f.failed = &in
	f.installation.Status = model.InstallationError
	return nil
}
func (f *repositoryFake) FailInstallation(context.Context, string, string, string, string) error {
	f.installation.Status = model.InstallationError
	return nil
}
func (f *repositoryFake) TransitionInstallation(_ context.Context, _, _ string, target, _, _ string, _ int64) (model.Installation, error) {
	f.installation.Status = target
	f.installation.Version++
	return f.installation, nil
}
func (f *repositoryFake) UpgradeInstallation(_ context.Context, _, _, providerVersion, _, _ string, _ int64) (model.Installation, error) {
	f.installation.ProviderVersion = providerVersion
	f.installation.Status = model.InstallationConfigRequired
	f.installation.EngineConnectorID = ""
	f.installation.Version++
	return f.installation, nil
}
func (f *repositoryFake) HasOpenRefundLiability(context.Context, string, string) (bool, error) {
	return f.refundLiability, nil
}
func (f *repositoryFake) DeleteProviderCredentials(context.Context, string, string) error {
	f.ciphertext = nil
	f.credentialsGone = true
	return nil
}
func (f *repositoryFake) MarkUninstalled(context.Context, string, string, string, string) (model.Installation, error) {
	f.installation.Status = model.InstallationUninstalled
	return f.installation, nil
}

type engineFake struct {
	manifest        connector.Manifest
	result          connector.InstallationResult
	verifyErr       error
	verification    connector.InstallationInput
	manifestCode    string
	manifestVersion string
	disableCalled   bool
}

func (f *engineFake) ManifestVersion(code, version string) (connector.Manifest, error) {
	f.manifestCode, f.manifestVersion = code, version
	return f.manifest, nil
}
func (f *engineFake) VerifyInstallation(_ context.Context, in connector.InstallationInput) (connector.InstallationResult, error) {
	f.verification = in
	return f.result, f.verifyErr
}
func (f *engineFake) DisableInstallation(context.Context, connector.InstallationInput) error {
	f.disableCalled = true
	return nil
}

type cipherFake struct{}

func (cipherFake) Encrypt(plaintext, _ []byte) ([]byte, error) {
	return append([]byte("encrypted:"), plaintext...), nil
}
func (cipherFake) Decrypt(ciphertext, _ []byte) ([]byte, error) {
	if !strings.HasPrefix(string(ciphertext), "encrypted:") {
		return nil, errors.New("invalid ciphertext")
	}
	return append([]byte(nil), ciphertext[len("encrypted:"):]...), nil
}

func installationFixture() (*repositoryFake, *engineFake, *Service) {
	digest := strings.Repeat("a", 64)
	repository := &repositoryFake{
		provider: model.Provider{
			Code: "xendit",
			CredentialSchema: json.RawMessage(`[
				{"code":"api_key","secret":true,"required":true},
				{"code":"webhook_token","secret":true,"required":false}
			]`),
		},
		installation: model.Installation{
			ID: "ins_test", TenantID: "merchant_test", ProviderCode: "xendit",
			ProviderVersion: "emisell-xendit-v1", Environment: model.EnvironmentSandbox,
			Status: model.InstallationConfigRequired, Version: 1,
			PaymentMethods: json.RawMessage(`[]`),
		},
	}
	engine := &engineFake{
		manifest: connector.Manifest{Code: "xendit", Version: "emisell-xendit-v1", ExecutableSHA256: digest},
		result: connector.InstallationResult{
			ConnectorID: "xendit:ins_test", Environment: model.EnvironmentSandbox,
			StoredCredentials: map[string]string{"api_key": "xnd_development_secret"}, WebhookReady: true,
		},
	}
	service := New(repository, engine, cipherFake{}, "https://payments.example.test/")
	service.now = func() time.Time { return time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC) }
	service.newID = func(prefix string) (string, error) { return prefix + "_test", nil }
	return repository, engine, service
}

func TestConfigureVerifiesEncryptsAndPersistsEvidence(t *testing.T) {
	repository, engine, service := installationFixture()
	item, err := service.Configure(context.Background(), ConfigureInput{
		TenantID: "merchant_test", InstallationID: "ins_test",
		Credentials:    map[string]string{"api_key": "xnd_development_secret"},
		PaymentMethods: []map[string]any{{"code": "qris"}}, PaymentMethodsPresent: true,
		Actor: "emisell-backend", RequestID: "req_test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != model.InstallationReady || repository.completed == nil {
		t.Fatalf("installation was not completed with durable evidence: %#v", item)
	}
	if repository.completed.ProviderVersion != "emisell-xendit-v1" || repository.completed.ManifestDigest != strings.Repeat("a", 64) {
		t.Fatalf("runtime evidence was not pinned: %#v", repository.completed)
	}
	if engine.verification.ProviderVersion != "emisell-xendit-v1" || engine.verification.PublicWebhookURL != "https://payments.example.test/webhooks/v1/providers/xendit/ins_test" {
		t.Fatalf("verification was dispatched with the wrong runtime binding: %#v", engine.verification)
	}
	if strings.Contains(string(repository.completed.Metadata), "xnd_development_secret") {
		t.Fatal("credential leaked into installation metadata")
	}
	var metadata map[string]any
	if err = json.Unmarshal(repository.completed.Metadata, &metadata); err != nil {
		t.Fatal(err)
	}
	verification, ok := metadata["verification"].(map[string]any)
	if !ok || verification["result"] != "PASSED" || metadata["verification_required"] != false {
		t.Fatalf("verification metadata is incomplete: %#v", metadata)
	}
}

func TestConfigureModeMismatchRecordsFailedEvidence(t *testing.T) {
	repository, engine, service := installationFixture()
	engine.result.Environment = model.EnvironmentLive
	_, err := service.Configure(context.Background(), ConfigureInput{
		TenantID: "merchant_test", InstallationID: "ins_test",
		Credentials: map[string]string{"api_key": "xnd_production_secret"},
		Actor:       "emisell-backend", RequestID: "req_test",
	})
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != CodeCredentialModeMismatch {
		t.Fatalf("got %v, want %s", err, CodeCredentialModeMismatch)
	}
	if repository.failed == nil || repository.failed.ErrorCode != CodeCredentialModeMismatch || repository.installation.Status != model.InstallationError {
		t.Fatalf("failed verification evidence was not recorded: %#v", repository.failed)
	}
}

func TestUpgradeRequiresConfigurationBeforeReactivation(t *testing.T) {
	repository, engine, service := installationFixture()
	repository.installation.Status = model.InstallationInactive
	repository.installation.EngineConnectorID = "xendit:ins_test"
	item, err := service.Upgrade(context.Background(), UpgradeInput{
		TenantID: "merchant_test", InstallationID: "ins_test",
		ProviderVersion: "emisell-xendit-v2", ExpectedVersion: 1,
		Actor: "emisell-backend", RequestID: "req_upgrade",
	})
	if err != nil {
		t.Fatal(err)
	}
	if engine.manifestVersion != "emisell-xendit-v2" {
		t.Fatalf("target runtime was not checked: %q", engine.manifestVersion)
	}
	if item.Status != model.InstallationConfigRequired || item.EngineConnectorID != "" {
		t.Fatalf("upgrade bypassed credential re-verification: %#v", item)
	}
}

func TestUninstallRejectsOpenRefundLiability(t *testing.T) {
	repository, engine, service := installationFixture()
	repository.installation.Status = model.InstallationInactive
	repository.refundLiability = true
	_, err := service.Uninstall(context.Background(), UninstallInput{
		TenantID: "merchant_test", InstallationID: "ins_test", Actor: "emisell-backend",
	})
	var domainErr *Error
	if !errors.As(err, &domainErr) || domainErr.Code != CodeRefundLiabilityOpen {
		t.Fatalf("got %v, want %s", err, CodeRefundLiabilityOpen)
	}
	if engine.disableCalled || repository.credentialsGone {
		t.Fatal("uninstall changed provider state while refund liability was open")
	}
}
