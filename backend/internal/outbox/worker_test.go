package outbox

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
)

func TestUnsafeIP(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.1.2.3", "192.168.1.2", "169.254.1.1", "::1"} {
		if !unsafeIP(net.ParseIP(value)) {
			t.Fatalf("expected %s to be rejected", value)
		}
	}
	if unsafeIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP was rejected")
	}
}

func TestDeliveryRequestUsesCanonicalEnvelopeAndSignature(t *testing.T) {
	secret := "test-secret-that-is-at-least-thirty-two-characters"
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	body, err := emisellwebhook.MarshalEnvelope("evt_test", "payment.updated", "merchant_test", "payment", "pay_test", now, map[string]any{
		"payment": map[string]any{"id": "pay_test", "status": "SUCCEEDED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := newDeliveryRequest(context.Background(), "https://api.emisell.test/webhooks/v1/payment-proxy", []byte(secret), "evt_test", "merchant_test", "payment.updated", body, now)
	if err != nil {
		t.Fatal(err)
	}
	if request.Header.Get(emisellwebhook.HeaderWebhookID) != "evt_test" || request.Header.Get("Idempotency-Key") != "evt_test" || request.Header.Get(emisellwebhook.HeaderWebhookVersion) != "1" {
		t.Fatalf("unexpected delivery headers: %v", request.Header)
	}
	timestamp, err := strconv.ParseInt(request.Header.Get(emisellwebhook.HeaderWebhookTimestamp), 10, 64)
	if err != nil || !emisellwebhook.VerifySignature([]byte(secret), timestamp, body, request.Header.Get(emisellwebhook.HeaderWebhookSignature)) {
		t.Fatal("delivery signature is invalid")
	}
}

func TestDeliveryRejectsMismatchedEnvelopeAndClassifiesFailures(t *testing.T) {
	body, err := emisellwebhook.MarshalEnvelope("evt_body", "payment.updated", "merchant_test", "payment", "pay_test", time.Now(), map[string]any{"payment": map[string]string{"id": "pay_test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = newDeliveryRequest(context.Background(), "https://api.emisell.test/webhooks/v1/payment-proxy", []byte("test-secret-that-is-at-least-thirty-two-characters"), "evt_header", "merchant_test", "payment.updated", body, time.Now()); err == nil {
		t.Fatal("mismatched envelope was accepted")
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound, http.StatusMovedPermanently} {
		if !permanentHTTPFailure(status) {
			t.Fatalf("HTTP %d should be permanent", status)
		}
	}
	for _, status := range []int{0, http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		if permanentHTTPFailure(status) {
			t.Fatalf("HTTP %d should be retryable", status)
		}
	}
}
