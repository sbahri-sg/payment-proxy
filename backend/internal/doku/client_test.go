package doku

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	testClientID  = "MCH-0001"
	testSecretKey = "doku-test-secret"
)

func TestVerifyInstallationSignsDOKUStatusRequestWithoutDigest(t *testing.T) {
	fixed := time.Date(2026, 9, 1, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, checkStatusPathPrefix+"EMSVERIFY") {
			t.Fatalf("unexpected verification path %s", request.URL.Path)
		}
		expected := requestSignature(apiCredentials{clientID: testClientID, secretKey: testSecretKey}, request.Header.Get("Request-Id"), fixed.Format("2006-01-02T15:04:05Z"), request.URL.Path, nil)
		if request.Header.Get("Request-Timestamp") != fixed.Format("2006-01-02T15:04:05Z") || request.Header.Get("Signature") != expected || strings.Contains(request.Header.Get("Signature"), "Digest:") {
			t.Fatalf("invalid DOKU verification signature: %#v", request.Header)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error_code":"ORDER_NOT_FOUND"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	client.now = func() time.Time { return fixed }
	result, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_doku_1", Environment: "sandbox",
		Credentials:      map[string]string{"client_id": testClientID, "secret_key": testSecretKey},
		PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/doku/ins_doku_1",
	})
	if err != nil || result.ConnectorID != "doku:ins_doku_1" || !result.WebhookReady {
		t.Fatalf("unexpected installation result: %#v, %v", result, err)
	}
}

func TestHostedCheckoutReturnsOfficialDOKUPaymentURL(t *testing.T) {
	fixed := time.Date(2026, 9, 1, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		expected := requestSignature(apiCredentials{clientID: testClientID, secretKey: testSecretKey}, request.Header.Get("Request-Id"), fixed.Format("2006-01-02T15:04:05Z"), checkoutPaymentPath, body)
		if request.URL.Path != checkoutPaymentPath || request.Header.Get("Signature") != expected {
			t.Fatalf("invalid DOKU checkout request: %s %#v", request.URL.Path, request.Header)
		}
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		payment := nestedMap(payload, "payment")
		if _, exists := payment["payment_method_types"]; exists {
			t.Fatalf("hosted checkout must let DOKU show merchant-active methods: %#v", payment)
		}
		order := nestedMap(payload, "order")
		if order["amount"] != float64(10000) || order["currency"] != "IDR" || order["callback_url"] != "https://shop.example.com/return" {
			t.Fatalf("unexpected DOKU order: %#v", order)
		}
		_, _ = io.WriteString(w, `{"response":{"order":{"invoice_number":"EMS123","session_id":"SESSION-1"},"payment":{"url":"https://checkout.doku.com/checkout/abc","token_id":"TOKEN-1"}}}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	client.now = func() time.Time { return fixed }
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", CheckoutMode: connector.CheckoutModeProviderHosted,
		Credentials:    map[string]string{"client_id": testClientID, "secret_key": testSecretKey},
		LocalPaymentID: "pay_doku_1", IdempotencyKey: "idem_doku_1", Amount: 1_000_000, Currency: "IDR",
		ReturnURL: "https://shop.example.com/return", PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/doku/ins_doku_1",
	})
	if err != nil || result.ID != "EMS123" || result.Status != "REQUIRES_ACTION" || result.ConnectorTransactionID != "SESSION-1" || !strings.Contains(string(result.NextAction), "checkout.doku.com") {
		t.Fatalf("unexpected DOKU payment: %#v, %v", result, err)
	}
}

func TestDirectCheckoutSelectsOneDOKUChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(request.Body).Decode(&payload)
		methods, _ := nestedMap(payload, "payment")["payment_method_types"].([]any)
		if len(methods) != 1 || methods[0] != "VIRTUAL_ACCOUNT_BCA" {
			t.Fatalf("va_bca must select only DOKU BCA VA: %#v", payload)
		}
		_, _ = io.WriteString(w, `{"response":{"order":{"invoice_number":"EMS123"},"payment":{"url":"https://checkout.doku.com/bca","token_id":"TOKEN-1"}}}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"client_id": testClientID, "secret_key": testSecretKey},
		LocalPaymentID: "pay_bca", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "va_bca",
		ReturnURL: "https://shop.example.com/return", PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/doku/ins_1",
	})
	if err != nil || result.ConnectorTransactionID != "TOKEN-1" {
		t.Fatalf("unexpected direct checkout: %#v, %v", result, err)
	}
}

func TestGetPaymentKeepsChannelFailurePendingUntilOrderExpires(t *testing.T) {
	response := `{"order":{"status":"ORDER_ACTIVE"},"transaction":{"status":"FAILED","original_request_id":"REQ-1"}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, response) }))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.GetPayment(context.Background(), connector.PaymentLookup{Environment: "sandbox", PaymentID: "EMS123", Credentials: map[string]string{"client_id": testClientID, "secret_key": testSecretKey}})
	if err != nil || result.Status != "PENDING" || result.ConnectorTransactionID != "REQ-1" {
		t.Fatalf("unexpected DOKU status: %#v, %v", result, err)
	}
}

func TestWebhookVerifiesDOKUNonSNAPSignature(t *testing.T) {
	target := "/webhooks/v1/providers/doku/ins_doku_1"
	body := []byte(`{"order":{"invoice_number":"EMS123","status":"ORDER_ACTIVE"},"transaction":{"status":"SUCCESS"}}`)
	headers := http.Header{"Client-Id": []string{testClientID}, "Request-Id": []string{"notification-1"}, "Request-Timestamp": []string{"2026-09-01T03:04:05Z"}, webhookRequestTargetHeader: []string{target}}
	headers.Set("Signature", requestSignature(apiCredentials{clientID: testClientID, secretKey: testSecretKey}, "notification-1", "2026-09-01T03:04:05Z", target, body))
	client, _ := New("https://api-sandbox.doku.com", "https://api.doku.com", time.Second)
	event, err := client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"client_id": testClientID, "secret_key": testSecretKey}, Headers: headers, Body: body})
	if err != nil || event.ID != "notification-1" || event.PaymentID != "EMS123" || event.Status != "SUCCEEDED" {
		t.Fatalf("unexpected webhook: %#v, %v", event, err)
	}
	headers.Set("Signature", "HMACSHA256=invalid")
	_, err = client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"client_id": testClientID, "secret_key": testSecretKey}, Headers: headers, Body: body})
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_WEBHOOK_SIGNATURE" {
		t.Fatalf("invalid DOKU webhook was accepted: %v", err)
	}
}

func TestManifestMatchesSafeDOKUCheckoutScope(t *testing.T) {
	client, _ := New("", "", time.Second)
	manifest := client.Manifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "emisell-doku-v1.0.0" || len(manifest.CertificationProfiles) != 20 || !manifest.Supports(connector.OperationCreateHostedCheckout) {
		t.Fatalf("unexpected DOKU manifest: %#v", manifest)
	}
	for code, mapping := range supportedMethods {
		if err := client.ValidatePaymentMethod(connector.PaymentMethodMapping{PaymentMethodCode: code, ProviderMethod: mapping.providerMethod, ProviderMethodType: mapping.providerType}); err != nil {
			t.Fatalf("invalid catalog mapping %s: %v", code, err)
		}
	}
	for _, code := range []string{"paylater_akulaku", "paylater_kredivo", "paylater_indodana", "direct_debit_bri", "digital_banking_jenius", "kki"} {
		if err := client.ValidatePayment(connector.PaymentValidation{PaymentMethodCode: code, Amount: 1_000_000, Currency: "IDR"}); err == nil {
			t.Fatalf("%s must stay documented until its extra contract fields exist", code)
		}
	}
}
