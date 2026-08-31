package config

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestLoadAllowsMigrationOnlyConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "")
	t.Setenv("SERVICE_API_KEY", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("migration configuration should load: %v", err)
	}
	if err := cfg.ValidateRuntime(); err == nil {
		t.Fatal("API runtime validation should reject missing secrets")
	}
}

func TestLoadRejectsInvalidDatabasePoolBounds(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("DATABASE_MIN_CONNECTIONS", "50")
	t.Setenv("DATABASE_MAX_CONNECTIONS", "10")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "DATABASE_MIN_CONNECTIONS") {
		t.Fatalf("invalid pool bounds were accepted: %v", err)
	}
}

func TestLoadConfiguresProductionTrafficGuards(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("API_RATE_LIMIT_RPS", "")
	t.Setenv("API_RATE_LIMIT_BURST", "")
	t.Setenv("API_MAX_IN_FLIGHT", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIRateLimitRPS != 300 || cfg.APIRateLimitBurst != 600 || cfg.APIMaxInFlight != 500 {
		t.Fatalf("unexpected production traffic defaults: %#v", cfg)
	}
}

func TestLoadRejectsPartialRateLimitConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("API_RATE_LIMIT_RPS", "10")
	t.Setenv("API_RATE_LIMIT_BURST", "0")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "API_RATE_LIMIT") {
		t.Fatalf("partial rate limit configuration was accepted: %v", err)
	}
}

func TestLoadConfiguresMultipleConnectorRuntimes(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("CONNECTOR_RUNNER_BASE_URLS", "http://xendit-app:18081,http://midtrans-app:18083")
	t.Setenv("CONNECTOR_RUNNER_TOKENS", "xendit-token,midtrans-token")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ConnectorRunners) != 2 || cfg.ConnectorRunners[1].BaseURL != "http://midtrans-app:18083" || cfg.ConnectorRunners[1].Token != "midtrans-token" {
		t.Fatalf("unexpected connector runtimes: %#v", cfg.ConnectorRunners)
	}
}

func TestLoadRejectsConnectorRuntimeTokenCountMismatch(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("CONNECTOR_RUNNER_BASE_URLS", "http://xendit-app:18081,http://midtrans-app:18083")
	t.Setenv("CONNECTOR_RUNNER_TOKENS", "only-one-token")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "one token") {
		t.Fatalf("connector runtime token mismatch was accepted: %v", err)
	}
}

func TestProductionRuntimeRejectsWeakSecretsAndUnsafeWebhookNetwork(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("CONNECTOR_RUNNER_TOKEN", strings.Repeat("r", 40))
	t.Setenv("CONNECTOR_RUNNER_BASE_URL", "https://connector-runner.example")
	key := sha256.Sum256([]byte("production-test-credential-key"))
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key[:]))
	t.Setenv("SERVICE_API_KEY", "short")
	t.Setenv("ADMIN_API_KEY", strings.Repeat("a", 40))
	t.Setenv("PAYMENT_PROXY_PUBLIC_BASE_URL", "https://payments.example")
	t.Setenv("CERTIFICATION_RETURN_URL", "https://dashboard.example/return")
	t.Setenv("WEBHOOK_ALLOW_INSECURE_HTTP", "false")
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_NETWORKS", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateRuntime(); err == nil || !strings.Contains(err.Error(), "SERVICE_API_KEY") {
		t.Fatalf("weak production service key was accepted: %v", err)
	}

	t.Setenv("SERVICE_API_KEY", strings.Repeat("s", 40))
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_NETWORKS", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateRuntime(); err == nil || !strings.Contains(err.Error(), "webhook") {
		t.Fatalf("unsafe production webhook network was accepted: %v", err)
	}
}

func TestProductionRuntimeRejectsUniformEncryptionKey(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("CONNECTOR_RUNNER_TOKEN", strings.Repeat("r", 40))
	t.Setenv("CONNECTOR_RUNNER_BASE_URL", "https://connector-runner.example")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("SERVICE_API_KEY", strings.Repeat("s", 40))
	t.Setenv("ADMIN_API_KEY", strings.Repeat("a", 40))
	t.Setenv("PAYMENT_PROXY_PUBLIC_BASE_URL", "https://payments.example")
	t.Setenv("CERTIFICATION_RETURN_URL", "https://dashboard.example/return")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateRuntime(); err == nil || !strings.Contains(err.Error(), "randomly generated") {
		t.Fatalf("uniform production encryption key was accepted: %v", err)
	}
}

func TestProductionRuntimeAcceptsHardenedConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("APP_ENV", "production")
	t.Setenv("CONNECTOR_RUNNER_TOKEN", strings.Repeat("r", 40))
	t.Setenv("CONNECTOR_RUNNER_BASE_URL", "https://connector-runner.example")
	key := sha256.Sum256([]byte("production-test-credential-key"))
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key[:]))
	t.Setenv("SERVICE_API_KEY", strings.Repeat("s", 40))
	t.Setenv("ADMIN_API_KEY", strings.Repeat("a", 40))
	t.Setenv("PAYMENT_PROXY_PUBLIC_BASE_URL", "https://payments.example")
	t.Setenv("CERTIFICATION_RETURN_URL", "https://dashboard.example/return")
	t.Setenv("WEBHOOK_ALLOW_INSECURE_HTTP", "false")
	t.Setenv("WEBHOOK_ALLOW_PRIVATE_NETWORKS", "false")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.ValidateRuntime(); err != nil {
		t.Fatalf("hardened production configuration was rejected: %v", err)
	}
}

func TestLoadValidatesPaymentProxyPublicURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("PAYMENT_PROXY_PUBLIC_BASE_URL", "http://localhost:8080")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "public HTTPS") {
		t.Fatalf("insecure public URL was not rejected: %v", err)
	}

	t.Setenv("PAYMENT_PROXY_PUBLIC_BASE_URL", "https://payments.example")
	cfg, err := Load()
	if err != nil || cfg.PublicBaseURL != "https://payments.example" {
		t.Fatalf("valid public URL was rejected: %#v, %v", cfg, err)
	}
}

func TestLoadValidatesCertificationReturnURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("CERTIFICATION_RETURN_URL", "http://localhost:13000/certifications/return")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CERTIFICATION_RETURN_URL") {
		t.Fatalf("insecure certification return URL was not rejected: %v", err)
	}

	t.Setenv("CERTIFICATION_RETURN_URL", "https://dashboard.example/certifications/return")
	cfg, err := Load()
	if err != nil || cfg.CertificationReturnURL != "https://dashboard.example/certifications/return" {
		t.Fatalf("valid certification return URL was rejected: %#v, %v", cfg, err)
	}
}
