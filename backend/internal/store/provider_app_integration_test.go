package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCreateProviderAppProviderForAvailableCatalogProviderIntegration(t *testing.T) {
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

	providerCode := fmt.Sprintf("papp_itest_%d", time.Now().UnixNano())
	_, err = pool.Exec(ctx, `
		INSERT INTO providers(code,name,description,available,engine_connector,credential_schema,environments,payment_methods)
		VALUES($1,'Existing Provider','Already loaded by the runtime.',true,$1,'[]'::jsonb,'["sandbox"]'::jsonb,'[]'::jsonb)
	`, providerCode)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_app_providers WHERE provider_code=$1`, providerCode)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM providers WHERE code=$1`, providerCode)
	})

	registry := New(pool)
	if _, err = registry.ListProviders(ctx, ""); err != nil {
		t.Fatalf("provider catalog with providers outside the release registry could not be listed: %v", err)
	}
	input := CreateProviderAppProviderInput{
		ProviderCode:    providerCode,
		ProviderName:    "Existing Provider",
		Description:     "Registered for immutable Provider App versions.",
		Actor:           "integration-test",
		RequestID:       "request-provider-app-integration",
		Logo:            []byte("test-logo"),
		LogoContentType: "image/png",
	}
	created, err := registry.CreateProviderAppProvider(ctx, input)
	if err != nil {
		t.Fatalf("available catalog provider could not be registered as a Provider App: %v", err)
	}
	if created.ProviderCode != providerCode || created.Status != "DRAFT" || !created.HasLogo {
		t.Fatalf("unexpected provider registry result: %#v", created)
	}
	logo, logoContentType, err := registry.GetProviderLogo(ctx, providerCode)
	if err != nil || !bytes.Equal(logo, input.Logo) || logoContentType != input.LogoContentType {
		t.Fatalf("provider logo was not stored: %q, %q, %v", logo, logoContentType, err)
	}

	var available bool
	if err = pool.QueryRow(ctx, `SELECT available FROM providers WHERE code=$1`, providerCode).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("Provider App registration disabled an already available connector")
	}
	if _, err = registry.CreateProviderAppProvider(ctx, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate Provider App registration returned %v, want ErrConflict", err)
	}
}

func TestTransitionDraftProviderAppProviderIntegration(t *testing.T) {
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

	providerCode := fmt.Sprintf("papp_state_%d", time.Now().UnixNano())
	registry := New(pool)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit_logs WHERE resource_type='provider' AND resource_id=$1`, providerCode)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_versions WHERE provider_code=$1`, providerCode)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_app_versions WHERE provider_code=$1`, providerCode)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_app_providers WHERE provider_code=$1`, providerCode)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM providers WHERE code=$1`, providerCode)
	})
	created, err := registry.CreateProviderAppProvider(ctx, CreateProviderAppProviderInput{
		ProviderCode: providerCode, ProviderName: "State Test Provider", Description: "Provider lifecycle test.",
		Actor: "integration-test", RequestID: "request-provider-state-create",
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := registry.TransitionProviderAppProvider(ctx, TransitionProviderAppProviderInput{
		ProviderCode: providerCode, ExpectedStatus: created.Status, Status: "DISABLED",
		Actor: "integration-test", RequestID: "request-provider-state-disable",
	})
	if err != nil || disabled.Status != "DISABLED" {
		t.Fatalf("draft provider was not disabled: %#v, %v", disabled, err)
	}
	var available bool
	if err = pool.QueryRow(ctx, `SELECT available FROM providers WHERE code=$1`, providerCode).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatal("disabled provider remained available for new installations")
	}
	restored, err := registry.TransitionProviderAppProvider(ctx, TransitionProviderAppProviderInput{
		ProviderCode: providerCode, ExpectedStatus: disabled.Status, Status: "DRAFT",
		Actor: "integration-test", RequestID: "request-provider-state-enable",
	})
	if err != nil || restored.Status != "DRAFT" {
		t.Fatalf("disabled draft provider was not restored: %#v, %v", restored, err)
	}
	providerAppID := fmt.Sprintf("papp_%d", time.Now().UnixNano())
	providerVersionID := fmt.Sprintf("pver_%d", time.Now().UnixNano())
	if _, err = pool.Exec(ctx, `
		INSERT INTO provider_app_versions(
			id,provider_code,provider_name,version,status,runtime,sdk_version,file_name,
			artifact_size,artifact_sha256,artifact,manifest,scan_report,submitted_by,published_at
		) VALUES($1,$2,'State Test Provider','1.0.0','PUBLISHED','isolated_container','v1',
		         'state-test.zip',1,$3,'\x01','{}'::jsonb,'{}'::jsonb,'integration-test',now())
	`, providerAppID, providerCode, fmt.Sprintf("%064d", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO provider_versions(id,provider_code,version,engine_kind,artifact_digest,status,released_at)
		VALUES($1,$2,'1.0.0','isolated_container',$3,'RELEASED',now())
	`, providerVersionID, providerCode, fmt.Sprintf("%064d", 0)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE provider_app_providers SET status='ACTIVE' WHERE provider_code=$1`, providerCode); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE providers SET available=true WHERE code=$1`, providerCode); err != nil {
		t.Fatal(err)
	}
	disabled, err = registry.TransitionProviderAppProvider(ctx, TransitionProviderAppProviderInput{
		ProviderCode: providerCode, ExpectedStatus: "ACTIVE", Status: "DISABLED",
		Actor: "integration-test", RequestID: "request-active-provider-disable",
	})
	if err != nil || disabled.Status != "DISABLED" {
		t.Fatalf("active provider was not disabled: %#v, %v", disabled, err)
	}
	if _, transitionErr := registry.TransitionProviderApp(ctx, ProviderAppTransitionInput{
		ID: providerAppID, ExpectedStatus: "PUBLISHED", Status: "DEPRECATED",
		Actor: "integration-test", RequestID: "request-disabled-provider-release-transition",
	}); !errors.Is(transitionErr, ErrInvalidState) {
		t.Fatalf("disabled provider allowed a release transition: %v", transitionErr)
	}
	restored, err = registry.TransitionProviderAppProvider(ctx, TransitionProviderAppProviderInput{
		ProviderCode: providerCode, ExpectedStatus: "DISABLED", Status: "ACTIVE",
		Actor: "integration-test", RequestID: "request-active-provider-enable",
	})
	if err != nil || restored.Status != "ACTIVE" {
		t.Fatalf("published provider was not re-enabled: %#v, %v", restored, err)
	}
	if err = pool.QueryRow(ctx, `SELECT available FROM providers WHERE code=$1`, providerCode).Scan(&available); err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("re-enabled published provider did not return to the installation catalog")
	}
}
