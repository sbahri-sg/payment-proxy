package webhooksettings

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
	"github.com/emisell/api-payment-proxy/internal/secrets"
)

type memoryRepository struct {
	stored     StoredSettings
	configured bool
}

func (r *memoryRepository) Get(context.Context) (StoredSettings, error) {
	if !r.configured {
		return StoredSettings{}, ErrNotConfigured
	}
	return r.stored, nil
}

func (r *memoryRepository) UpsertConfig(_ context.Context, callbackURL string, enabled bool, actor string) error {
	r.configured = true
	r.stored.Configured = true
	r.stored.CallbackURL = callbackURL
	r.stored.Enabled = enabled
	r.stored.Source = "database"
	r.stored.UpdatedBy = actor
	return nil
}

func (r *memoryRepository) RotateSecret(_ context.Context, input SecretInput) error {
	r.configured = true
	r.stored.SecretCiphertext = append([]byte(nil), input.Ciphertext...)
	r.stored.SecretFingerprint = append([]byte(nil), input.Fingerprint...)
	r.stored.SecretConfigured = true
	r.stored.SecretHint = maskedSecretHint(input.Prefix, input.LastFour)
	r.stored.Source = "database"
	return nil
}

func (r *memoryRepository) RecordTest(_ context.Context, at time.Time, success bool, status int, message string) error {
	r.stored.LastTestAt = &at
	r.stored.LastTestSuccess = &success
	if status > 0 {
		r.stored.LastTestHTTPStatus = &status
	}
	r.stored.LastTestError = message
	return nil
}

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte("01234567890123456789012345678901"))
	cipher, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func TestGenerateSecretIsOneTimeAndRotationDisablesDelivery(t *testing.T) {
	repository := &memoryRepository{}
	service := NewService(repository, testCipher(t), "development", true, true, time.Second, Fallback{
		Enabled: true, CallbackURL: "http://receiver.test/webhooks", Secret: "old-secret-that-is-at-least-thirty-two-characters",
	})
	generated, err := service.GenerateSecret(context.Background(), "dashboard:operator")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(generated.Secret, "whsec_") || len(generated.Secret) < 32 {
		t.Fatalf("invalid generated secret %q", generated.Secret)
	}
	if generated.Settings.Enabled {
		t.Fatal("rotation must disable delivery until receiver is updated")
	}
	if generated.Settings.SecretHint == generated.Secret || !strings.Contains(generated.Settings.SecretHint, "••••") {
		t.Fatalf("secret was not masked: %q", generated.Settings.SecretHint)
	}
	resolvedURL, resolvedSecret, enabled, err := service.ResolveWebhookDestination(context.Background())
	if err != nil || enabled || resolvedURL != "" || len(resolvedSecret) != 0 {
		t.Fatalf("disabled destination unexpectedly resolved: %q %t %v", resolvedURL, enabled, err)
	}
	settings, err := service.Update(context.Background(), "http://receiver.test/webhooks", true, "dashboard:operator")
	if err != nil || !settings.Enabled {
		t.Fatalf("could not re-enable generated secret: %#v %v", settings, err)
	}
	resolvedURL, resolvedSecret, enabled, err = service.ResolveWebhookDestination(context.Background())
	if err != nil || !enabled || resolvedURL == "" || string(resolvedSecret) != generated.Secret {
		t.Fatal("worker did not resolve the generated secret")
	}
}

func TestEnableRequiresGeneratedSecret(t *testing.T) {
	service := NewService(&memoryRepository{}, testCipher(t), "development", true, true, time.Second, Fallback{})
	_, err := service.Update(context.Background(), "http://receiver.test/webhooks", true, "operator")
	if !errors.Is(err, ErrSecretNotConfigured) {
		t.Fatalf("expected missing secret error, got %v", err)
	}
}

func TestConnectionTestUsesCanonicalSignedEnvelope(t *testing.T) {
	secret := "test-secret-that-is-at-least-thirty-two-characters"
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		timestamp, err := strconv.ParseInt(request.Header.Get(emisellwebhook.HeaderWebhookTimestamp), 10, 64)
		if err != nil || !emisellwebhook.VerifySignature([]byte(secret), timestamp, body, request.Header.Get(emisellwebhook.HeaderWebhookSignature)) {
			t.Error("invalid webhook test signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err = emisellwebhook.ParseAndValidate(body, request.Header.Get(emisellwebhook.HeaderWebhookID), "webhook.test", "platform"); err != nil {
			t.Errorf("invalid canonical test envelope: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer receiver.Close()

	repository := &memoryRepository{}
	service := NewService(repository, testCipher(t), "development", true, true, time.Second, Fallback{
		CallbackURL: receiver.URL, Secret: secret,
	})
	result, err := service.Test(context.Background(), "dashboard:operator")
	if err != nil || !result.Success || result.HTTPStatus != http.StatusAccepted {
		t.Fatalf("connection test failed: %#v %v", result, err)
	}
}
