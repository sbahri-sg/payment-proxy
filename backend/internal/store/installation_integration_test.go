package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestInstallationUpgradeAndVerificationEvidenceIntegration(t *testing.T) {
	databaseURL := os.Getenv("PAYMENT_PROXY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PAYMENT_PROXY_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	providerCode := "install_itest_" + suffix
	tenantID := "itest.installation." + suffix
	installationID := "ins_itest_" + suffix
	oldVersion, newVersion := "runtime-v1", "runtime-v2"
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit_logs WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM installation_verifications WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_credentials WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_installations WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_versions WHERE provider_code=$1`, providerCode)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM providers WHERE code=$1`, providerCode)
	})
	_, err = pool.Exec(ctx, `
		INSERT INTO providers(code,name,description,available,engine_connector,credential_schema,environments,payment_methods)
		VALUES($1,'Installation Test','Integration lifecycle test.',true,$1,
		       '[{"code":"api_key","secret":true,"required":true}]'::jsonb,
		       '["sandbox"]'::jsonb,'[]'::jsonb)
	`, providerCode)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_versions(id,provider_code,version,engine_kind,status,released_at)
		VALUES($2,$1,$3,'integration','RELEASED',now()),($4,$1,$5,'integration','RELEASED',now())
	`, providerCode, "pver_old_"+suffix, oldVersion, "pver_new_"+suffix, newVersion)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_installations(
			id,tenant_id,provider_code,provider_version,environment,engine_profile_id,
			engine_connector_id,status,credential_metadata,created_by,updated_by
		) VALUES($1,$2,$3,$4,'sandbox','integration','connector-old','INACTIVE',
		         '{"verified_environment":"sandbox","webhook_ready":true,"verification":{"result":"PASSED"}}'::jsonb,
		         'integration','integration')
	`, installationID, tenantID, providerCode, oldVersion)
	if err != nil {
		t.Fatal(err)
	}

	repository := New(pool)
	upgraded, err := repository.UpgradeInstallation(ctx, tenantID, installationID, newVersion, "integration", "req-upgrade", 1)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Status != model.InstallationConfigRequired || upgraded.EngineConnectorID != "" {
		t.Fatalf("upgrade bypassed re-verification: %#v", upgraded)
	}
	var verificationRequired bool
	if err = pool.QueryRow(ctx, `
		SELECT (credential_metadata->>'verification_required')::boolean
		FROM provider_installations WHERE tenant_id=$1 AND id=$2
	`, tenantID, installationID).Scan(&verificationRequired); err != nil {
		t.Fatal(err)
	}
	if !verificationRequired {
		t.Fatal("upgrade did not mark the new provider version as requiring verification")
	}

	if _, err = repository.BeginCredentialConfig(ctx, tenantID, installationID, "integration", "req-configure"); err != nil {
		t.Fatal(err)
	}
	if err = repository.SaveProviderCredentials(ctx, tenantID, installationID, []byte(strings.Repeat("x", 32))); err != nil {
		t.Fatal(err)
	}
	verifiedAt := time.Now().UTC().Truncate(time.Microsecond)
	ready, err := repository.CompleteInstallationVerification(ctx, CompleteInstallationVerificationInput{
		ID: "iver_passed_" + suffix, TenantID: tenantID, InstallationID: installationID,
		ProviderCode: providerCode, ProviderVersion: newVersion, Environment: model.EnvironmentSandbox,
		ManifestDigest: strings.Repeat("a", 64), ConnectorID: "connector-new",
		Metadata: []byte(`{"verification_required":false}`), PaymentMethods: []byte(`[]`),
		WebhookReady: true, Actor: "integration", RequestID: "req-configure", VerifiedAt: verifiedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ready.Status != model.InstallationReady || ready.EngineConnectorID != "connector-new" {
		t.Fatalf("passed evidence did not promote installation to READY: %#v", ready)
	}
	var result, evidenceVersion, digest string
	if err = pool.QueryRow(ctx, `
		SELECT result,provider_version,manifest_digest
		FROM installation_verifications WHERE id=$1
	`, "iver_passed_"+suffix).Scan(&result, &evidenceVersion, &digest); err != nil {
		t.Fatal(err)
	}
	if result != "PASSED" || evidenceVersion != newVersion || digest != strings.Repeat("a", 64) {
		t.Fatalf("unexpected verification evidence: result=%s version=%s digest=%s", result, evidenceVersion, digest)
	}

	if _, err = repository.BeginCredentialConfig(ctx, tenantID, installationID, "integration", "req-reverify"); err != nil {
		t.Fatal(err)
	}
	if err = repository.FailInstallationVerification(ctx, FailInstallationVerificationInput{
		ID: "iver_failed_" + suffix, TenantID: tenantID, InstallationID: installationID,
		ProviderCode: providerCode, ProviderVersion: newVersion, Environment: model.EnvironmentSandbox,
		ManifestDigest: strings.Repeat("a", 64), ErrorCode: "INVALID_PROVIDER_CREDENTIAL",
		ErrorMessage: "provider request failed", Actor: "integration", RequestID: "req-reverify", VerifiedAt: verifiedAt,
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := repository.GetInstallation(ctx, tenantID, installationID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != model.InstallationError {
		t.Fatalf("failed evidence did not move installation to ERROR: %#v", failed)
	}
}
