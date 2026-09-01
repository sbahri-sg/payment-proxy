package ipaymu

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	testVA     = "1179000899"
	testAPIKey = "ipaymu-test-api-key"
)

func TestVerifyInstallationUsesSignedPaymentChannelsProbe(t *testing.T) {
	fixed := time.Date(2026, 9, 1, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != paymentChannelsPath {
			t.Fatalf("unexpected iPaymu verification request: %s %s", request.Method, request.URL.Path)
		}
		credentials := apiCredentials{va: testVA, apiKey: testAPIKey}
		if request.Header.Get("va") != testVA || request.Header.Get("signature") != requestSignature(http.MethodGet, credentials, []byte("{}")) || request.Header.Get("timestamp") != "20260901030405" {
			t.Fatalf("invalid iPaymu verification headers: %#v", request.Header)
		}
		_, _ = io.WriteString(w, `{"Status":200,"Success":true,"Message":"Success","Data":[]}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	client.now = func() time.Time { return fixed }
	result, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_ipaymu_1", Environment: "sandbox",
		Credentials:      map[string]string{"va": testVA, "api_key": testAPIKey},
		PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/ipaymu/ins_ipaymu_1",
	})
	if err != nil || result.ConnectorID != "ipaymu:ins_ipaymu_1" || !result.WebhookReady {
		t.Fatalf("unexpected installation result: %#v, %v", result, err)
	}
}

func TestHostedCheckoutReturnsOfficialIPaymuPaymentURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if request.URL.Path != paymentPath || request.Header.Get("signature") != requestSignature(http.MethodPost, apiCredentials{va: testVA, apiKey: testAPIKey}, body) {
			t.Fatalf("invalid iPaymu hosted request: %s %#v", request.URL.Path, request.Header)
		}
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if _, exists := payload["paymentMethod"]; exists {
			t.Fatalf("hosted checkout must let iPaymu show merchant-active methods: %#v", payload)
		}
		if payload["referenceId"] != "order-ipaymu-1" || payload["notifyUrl"] != "https://payments.example.com/webhooks/v1/providers/ipaymu/ins_1" {
			t.Fatalf("unexpected hosted payload: %#v", payload)
		}
		_, _ = io.WriteString(w, `{"Status":200,"Message":"success","Data":{"SessionID":"SESSION-1","Url":"https://sandbox.ipaymu.com/payment/SESSION-1"}}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", CheckoutMode: connector.CheckoutModeProviderHosted,
		Credentials:    map[string]string{"va": testVA, "api_key": testAPIKey},
		LocalPaymentID: "pay_ipaymu_1", MerchantReference: "order-ipaymu-1", Amount: 10_000, Currency: "IDR",
		ReturnURL: "https://shop.example.com/return", PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/ipaymu/ins_1",
		Items: []connector.Item{{Name: "Subscription", NetUnitAmount: 10_000, Quantity: 1}},
	})
	if err != nil || result.ID != "order-ipaymu-1" || result.Status != "REQUIRES_ACTION" || result.ConnectorTransactionID != "SESSION-1" || !strings.Contains(string(result.NextAction), "sandbox.ipaymu.com/payment") {
		t.Fatalf("unexpected hosted checkout: %#v, %v", result, err)
	}
}

func TestDirectVirtualAccountReturnsNormalizedNextAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(request.Body).Decode(&payload)
		if request.URL.Path != directPaymentPath || payload["paymentMethod"] != "va" || payload["paymentChannel"] != "bca" || payload["amount"] != float64(10000) {
			t.Fatalf("unexpected direct payload: %#v", payload)
		}
		_, _ = io.WriteString(w, `{"Status":200,"Success":true,"Message":"Success","Data":{"TransactionId":12345,"ReferenceId":"order-1","Via":"va","Channel":"bca","PaymentNo":"1234567890","Total":10000}}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"va": testVA, "api_key": testAPIKey},
		LocalPaymentID: "pay_1", MerchantReference: "order-1", Amount: 10_000, Currency: "IDR",
		PaymentMethodCode: "va_bca", Customer: connector.Customer{Name: "Buyer", Email: "buyer@example.com", Phone: "08123456789"},
		ReturnURL: "https://shop.example.com/return", PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/ipaymu/ins_1",
	})
	if err != nil || result.ID != "order-1" || result.ConnectorTransactionID != "12345" || !strings.Contains(string(result.NextAction), "virtual_account_information") || !strings.Contains(string(result.NextAction), "1234567890") {
		t.Fatalf("unexpected direct payment: %#v, %v", result, err)
	}
}

func TestGetPaymentUsesOfficialReferenceLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"referenceId":"order-lookup-1"}` {
			t.Fatalf("status lookup did not use referenceId: %s", body)
		}
		_, _ = io.WriteString(w, `{"Status":200,"Success":true,"Message":"success","Data":{"TransactionId":4719,"ReferenceId":"order-lookup-1","Status":1,"StatusDesc":"Berhasil"}}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.GetPayment(context.Background(), connector.PaymentLookup{
		Environment: "sandbox", PaymentID: "order-lookup-1", Credentials: map[string]string{"va": testVA, "api_key": testAPIKey},
	})
	if err != nil || result.ID != "order-lookup-1" || result.Status != "SUCCEEDED" || result.ConnectorTransactionID != "4719" {
		t.Fatalf("unexpected payment lookup: %#v, %v", result, err)
	}
}

func TestWebhookValidatesOfficialFormSignature(t *testing.T) {
	form := url.Values{
		"trx_id":                  {"12345678"},
		"sid":                     {"SESSION789"},
		"reference_id":            {"order-webhook-1"},
		"status":                  {"berhasil"},
		"status_code":             {"1"},
		"transaction_status_code": {"1"},
		"paid_off":                {"98500"},
		"merchant":                {testVA},
		"url":                     {"https://payments.example.com/webhooks/v1/providers/ipaymu/ins_1"},
		"is_escrow":               {"0"},
	}
	body := []byte(form.Encode())
	payload, err := normalizedWebhookPayload("application/x-www-form-urlencoded", body)
	if err != nil {
		t.Fatal(err)
	}
	var encodedBuffer strings.Builder
	encoder := json.NewEncoder(&encodedBuffer)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(payload)
	encoded := strings.TrimSuffix(encodedBuffer.String(), "\n")
	escaped := strings.ReplaceAll(encoded, "/", "\\/")
	digest := hmac.New(sha256.New, []byte(testVA))
	_, _ = digest.Write([]byte(escaped))
	signature := hex.EncodeToString(digest.Sum(nil))
	headers := http.Header{
		"Content-Type":  {"application/x-www-form-urlencoded"},
		"X-Signature":   {signature},
		"X-External-Id": {"callback-1"},
		"X-Timestamp":   {"2026-09-01T10:10:52+07:00"},
	}
	client, _ := New("", "", time.Second)
	event, err := client.HandleWebhook(context.Background(), connector.WebhookInput{
		Credentials: map[string]string{"va": testVA, "api_key": testAPIKey}, Headers: headers, Body: body,
	})
	if err != nil || event.ID != "callback-1" || event.PaymentID != "order-webhook-1" || event.Status != "SUCCEEDED" {
		t.Fatalf("unexpected callback: %#v, %v", event, err)
	}
	headers.Set("X-Signature", "invalid")
	_, err = client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"va": testVA, "api_key": testAPIKey}, Headers: headers, Body: body})
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_WEBHOOK_SIGNATURE" {
		t.Fatalf("invalid callback signature was accepted: %v", err)
	}
}

func TestManifestMatchesSafeIPaymuScope(t *testing.T) {
	client, _ := New("", "", time.Second)
	manifest := client.Manifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "emisell-ipaymu-v2.0.1" || len(manifest.CertificationProfiles) != 18 || !manifest.Supports(connector.OperationCreateHostedCheckout) {
		t.Fatalf("unexpected iPaymu manifest: %#v", manifest)
	}
	for code, mapping := range supportedMethods {
		if err := client.ValidatePaymentMethod(connector.PaymentMethodMapping{PaymentMethodCode: code, ProviderMethod: mapping.providerMethod, ProviderMethodType: mapping.providerType}); err != nil {
			t.Fatalf("invalid catalog mapping %s: %v", code, err)
		}
	}
}
