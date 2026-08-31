package store

import (
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
	input := CreateProviderAppProviderInput{
		ProviderCode: providerCode,
		ProviderName: "Existing Provider",
		Description:  "Registered for immutable Provider App versions.",
		Actor:        "integration-test",
		RequestID:    "request-provider-app-integration",
	}
	created, err := registry.CreateProviderAppProvider(ctx, input)
	if err != nil {
		t.Fatalf("available catalog provider could not be registered as a Provider App: %v", err)
	}
	if created.ProviderCode != providerCode || created.Status != "DRAFT" {
		t.Fatalf("unexpected provider registry result: %#v", created)
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
