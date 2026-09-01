package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCatalogSearchIsCaseInsensitiveAndScoped(t *testing.T) {
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

	repository := New(pool)
	providers, err := repository.ListProviders(ctx, "XeNdIt")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) == 0 {
		t.Fatal("provider search did not return Xendit")
	}
	for _, provider := range providers {
		searchable := strings.ToLower(provider.Code + " " + provider.Name)
		if !strings.Contains(searchable, "xendit") {
			t.Fatalf("provider search returned an unrelated item: %#v", provider)
		}
	}

	methods, err := repository.ListPaymentMethods(ctx, "QrIs")
	if err != nil {
		t.Fatal(err)
	}
	if len(methods) == 0 {
		t.Fatal("payment method search did not return QRIS")
	}
	for _, method := range methods {
		searchable := strings.ToLower(method.Code + " " + method.Name + " " + method.Category + " " + method.Description)
		if !strings.Contains(searchable, "qris") {
			t.Fatalf("payment method search returned an unrelated item: %#v", method)
		}
	}

	missing, err := repository.ListProviders(ctx, "provider-that-does-not-exist-9f3c2b")
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing provider search returned %d items", len(missing))
	}
}
