package duitku

import (
	"context"
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

func TestVerifyInstallationUsesCurrentDuitkuHMAC(t *testing.T) {
	fixedTime := time.Date(2026, 9, 1, 3, 4, 5, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/merchant/paymentmethod/getpaymentmethod" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		merchantCode := "D12345"
		datetime := "2026-09-01 10:04:05"
		expected := sign("sandbox-api-key", merchantCode+"10000"+datetime)
		if body["merchantcode"] != merchantCode || body["datetime"] != datetime || body["signature"] != expected {
			t.Fatalf("unexpected verification payload: %#v", body)
		}
		_, _ = io.WriteString(w, `{"paymentFee":[],"responseCode":"00","responseMessage":"SUCCESS"}`)
	}))
	defer server.Close()
	client, err := New(server.URL, server.URL, server.URL, server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return fixedTime }
	result, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_duitku_1", Environment: "sandbox",
		Credentials:      map[string]string{"merchant_code": "D12345", "api_key": "sandbox-api-key"},
		PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/duitku/ins_duitku_1",
	})
	if err != nil || result.ConnectorID != "duitku:ins_duitku_1" || result.Environment != "sandbox" || !result.WebhookReady {
		t.Fatalf("unexpected installation result: %#v, %v", result, err)
	}
}

func TestHostedCheckoutUsesDuitkuPOPAndLeavesPaymentMethodEmpty(t *testing.T) {
	fixedTime := time.UnixMilli(1_788_200_000_123)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/merchant/createInvoice" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		timestamp := "1788200000123"
		if request.Header.Get("x-duitku-timestamp") != timestamp ||
			request.Header.Get("x-duitku-merchantcode") != "D12345" ||
			request.Header.Get("x-duitku-signature") != sign("api-key", "D12345"+timestamp) {
			t.Fatalf("unexpected Duitku authentication headers: %#v", request.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["merchantOrderId"] != "pay_duitku_1" || body["paymentAmount"] != float64(10000) || body["paymentMethod"] != "" {
			t.Fatalf("unexpected hosted checkout payload: %#v", body)
		}
		if body["returnUrl"] != "https://shop.example.com/payment/return" || body["callbackUrl"] != "https://payments.example.com/webhooks/duitku" {
			t.Fatalf("missing callback URLs: %#v", body)
		}
		_, _ = io.WriteString(w, `{"reference":"DUITKU-REF-1","paymentUrl":"https://app-sandbox.duitku.com/redirect/abc","statusCode":"00","statusMessage":"SUCCESS"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, server.URL, server.URL, time.Second)
	client.now = func() time.Time { return fixedTime }
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", CheckoutMode: connector.CheckoutModeProviderHosted,
		Credentials:    map[string]string{"merchant_code": "D12345", "api_key": "api-key"},
		LocalPaymentID: "pay_duitku_1", Amount: 1_000_000, Currency: "IDR",
		Customer:  connector.Customer{Name: "Budi", Email: "budi@example.com"},
		ReturnURL: "https://shop.example.com/payment/return", PublicWebhookURL: "https://payments.example.com/webhooks/duitku",
	})
	if err != nil || result.ID != "pay_duitku_1" || result.Status != "REQUIRES_ACTION" || result.ConnectorTransactionID != "DUITKU-REF-1" {
		t.Fatalf("unexpected payment result: %#v, %v", result, err)
	}
	if !strings.Contains(string(result.NextAction), "app-sandbox.duitku.com") {
		t.Fatalf("missing Duitku redirect: %s", result.NextAction)
	}
}

func TestDirectCheckoutMapsCanonicalMethodToDuitkuCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["paymentMethod"] != "BC" {
			t.Fatalf("va_bca must use Duitku code BC: %#v", body)
		}
		_, _ = io.WriteString(w, `{"reference":"REF-BC","paymentUrl":"https://app-sandbox.duitku.com/redirect/bca","statusCode":"00"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, server.URL, server.URL, time.Second)
	_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"merchant_code": "D12345", "api_key": "api-key"},
		LocalPaymentID: "pay_bca", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "va_bca",
		Customer: connector.Customer{Email: "budi@example.com"}, ReturnURL: "https://shop.example.com/return",
		PublicWebhookURL: "https://payments.example.com/webhooks/duitku",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetPaymentUsesTransactionStatusHMAC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/merchant/transactionStatus" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(request.Body).Decode(&body)
		if body["signature"] != sign("api-key", "D12345pay_1") {
			t.Fatalf("unexpected status signature: %#v", body)
		}
		_, _ = io.WriteString(w, `{"merchantOrderId":"pay_1","reference":"REF-1","statusCode":"00","statusMessage":"SUCCESS"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, server.URL, server.URL, time.Second)
	result, err := client.GetPayment(context.Background(), connector.PaymentLookup{
		Environment: "sandbox", PaymentID: "pay_1",
		Credentials: map[string]string{"merchant_code": "D12345", "api_key": "api-key"},
	})
	if err != nil || result.Status != "SUCCEEDED" || result.ConnectorTransactionID != "REF-1" {
		t.Fatalf("unexpected status result: %#v, %v", result, err)
	}
}

func TestClassicDuitkuEndpointsKeepWebAPIBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/webapi/api/merchant/transactionStatus" {
			t.Fatalf("classic Duitku endpoint lost /webapi prefix: %s", request.URL.Path)
		}
		_, _ = io.WriteString(w, `{"reference":"REF-1","statusCode":"01","statusMessage":"PENDING"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, server.URL+"/webapi", server.URL+"/webapi", time.Second)
	result, err := client.GetPayment(context.Background(), connector.PaymentLookup{
		Environment: "sandbox", PaymentID: "pay_1",
		Credentials: map[string]string{"merchant_code": "D12345", "api_key": "api-key"},
	})
	if err != nil || result.Status != "PENDING" {
		t.Fatalf("unexpected status result: %#v, %v", result, err)
	}
}

func TestWebhookUsesCurrentDuitkuHMAC(t *testing.T) {
	values := make(url.Values)
	values.Set("merchantCode", "D12345")
	values.Set("amount", "10000")
	values.Set("merchantOrderId", "pay_1")
	values.Set("resultCode", "00")
	values.Set("reference", "REF-1")
	values.Set("signature", sign("api-key", "D12345"+"10000"+"pay_1"))
	client, _ := New("https://api-sandbox.duitku.com", "https://api-prod.duitku.com", "https://sandbox.duitku.com/webapi", "https://passport.duitku.com/webapi", time.Second)
	event, err := client.HandleWebhook(context.Background(), connector.WebhookInput{
		Credentials: map[string]string{"merchant_code": "D12345", "api_key": "api-key"}, Body: []byte(values.Encode()),
	})
	if err != nil || event.ID != "REF-1:00" || event.PaymentID != "pay_1" || event.Status != "SUCCEEDED" {
		t.Fatalf("unexpected callback result: %#v, %v", event, err)
	}
	values.Set("signature", "invalid")
	_, err = client.HandleWebhook(context.Background(), connector.WebhookInput{
		Credentials: map[string]string{"merchant_code": "D12345", "api_key": "api-key"}, Body: []byte(values.Encode()),
	})
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "INVALID_WEBHOOK_SIGNATURE" {
		t.Fatalf("invalid callback signature was accepted: %v", err)
	}
}

func TestManifestMatchesCatalogAndKeepsUnsupportedMutationsClosed(t *testing.T) {
	client, _ := New("https://api-sandbox.duitku.com", "https://api-prod.duitku.com", "https://sandbox.duitku.com/webapi", "https://passport.duitku.com/webapi", time.Second)
	manifest := client.Manifest()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "emisell-duitku-v1.0.0" || !manifest.Supports(connector.OperationCreateHostedCheckout) {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
	for _, operation := range []connector.Operation{connector.OperationCancelPayment, connector.OperationCreateRefund, connector.OperationGetRefund} {
		if manifest.Supports(operation) {
			t.Fatalf("operation %s must remain closed until sandbox evidence exists", operation)
		}
	}
	for code, mapping := range supportedMethods {
		if err := client.ValidatePaymentMethod(connector.PaymentMethodMapping{
			PaymentMethodCode: code, ProviderMethod: mapping.providerMethod, ProviderMethodType: mapping.providerType,
		}); err != nil {
			t.Fatalf("catalog mapping %s is invalid: %v", code, err)
		}
	}
}
