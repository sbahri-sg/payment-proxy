package xendit

import (
	"context"
	"encoding/base64"
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

func TestNativeQRISPayment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("xnd_development_secret:"))
		if r.Header.Get("Authorization") != expectedAuth {
			t.Fatal("Xendit API key was not sent with Basic authentication")
		}
		switch r.URL.Path {
		case "/balance":
			_, _ = io.WriteString(w, `{"balance":100000}`)
		case "/v3/payment_requests":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["channel_code"] != "QRIS" || body["request_amount"] != float64(10000) {
				t.Fatalf("unexpected QRIS payload: %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":"pr-8877c08a-740d-4153-9816-3d744ed197a5","status":"PENDING","channel_code":"QRIS","actions":[{"type":"PRESENT_TO_CUSTOMER","descriptor":"QR_STRING","value":"000201010212"}]}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	installation, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{InstallationID: "ins_1", ProviderCode: "xendit", PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/xendit/ins_1", Credentials: map[string]string{"api_key": "xnd_development_secret", "webhook_verification_token": "callback-secret"}})
	if err != nil || installation.ConnectorID != "xendit:ins_1" {
		t.Fatalf("unexpected installation: %#v, %v", installation, err)
	}
	if !installation.WebhookReady {
		t.Fatal("installation with a public URL and callback token must be webhook-ready")
	}
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "xnd_development_secret"}, MerchantReference: "order-1",
		IdempotencyKey: "idem-12345678", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "qris", ChannelCode: "QRIS",
	})
	if err != nil || result.ID == "" || string(result.NextAction) == "" {
		t.Fatalf("unexpected payment: %#v, %v", result, err)
	}
}

func TestInstallationRequiresWebhookToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"balance":100000}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	_, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_1", Credentials: map[string]string{"api_key": "xnd_development_secret"},
	})
	if err == nil {
		t.Fatal("installation without a webhook verification token was accepted")
	}
}

func TestNativeBCAVirtualAccountMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		properties, _ := body["channel_properties"].(map[string]any)
		if body["channel_code"] != "BCA_VIRTUAL_ACCOUNT" || properties["display_name"] != "Budi Santoso" {
			t.Fatalf("unexpected BCA VA payload: %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"pr-8877c08a-740d-4153-9816-3d744ed197a5","status":"PENDING","channel_code":"BCA_VIRTUAL_ACCOUNT","actions":[{"type":"PRESENT_TO_CUSTOMER","descriptor":"VIRTUAL_ACCOUNT_NUMBER","value":"1234567890"}]}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "order-bca",
		IdempotencyKey: "idem-bca-1234", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "va_bca",
		ChannelCode: "BCA_VIRTUAL_ACCOUNT", Customer: connector.Customer{Name: "Budi Santoso"},
	})
	if err != nil || string(result.NextAction) == "" {
		t.Fatalf("unexpected result: %#v, %v", result, err)
	}
}

func TestNativeBRIVirtualAccountUsesCatalogChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["channel_code"] != "BRI_VIRTUAL_ACCOUNT" {
			t.Fatalf("unexpected VA payload: %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"pr-bri","status":"REQUIRES_ACTION","channel_code":"BRI_VIRTUAL_ACCOUNT","actions":[{"type":"PRESENT_TO_CUSTOMER","descriptor":"VIRTUAL_ACCOUNT_NUMBER","value":"888800001234"}]}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "order-bri", LocalPaymentID: "pay_bri",
		IdempotencyKey: "idem-bri-1234", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "va_bri",
		ChannelCode: "BRI_VIRTUAL_ACCOUNT", Customer: connector.Customer{Name: "Budi Santoso"},
	})
	if err != nil || !strings.Contains(string(result.NextAction), "virtual_account_information") {
		t.Fatalf("unexpected result: %#v, %v", result, err)
	}
}

func TestNativeEwalletMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		properties, _ := body["channel_properties"].(map[string]any)
		items, _ := body["items"].([]any)
		item, _ := items[0].(map[string]any)
		if body["channel_code"] != "DANA" || properties["success_return_url"] == "" || item["currency"] != "IDR" {
			t.Fatalf("unexpected e-wallet payload: %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"pr-dana","status":"REQUIRES_ACTION","channel_code":"DANA","actions":[{"type":"REDIRECT_CUSTOMER","descriptor":"WEB_URL","value":"https://example.test/authorize"}]}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "order-dana", LocalPaymentID: "pay_dana",
		IdempotencyKey: "idem-dana-1234", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "ewallet_dana", ChannelCode: "DANA",
		Customer:  connector.Customer{Name: "Budi Santoso", Email: "budi@example.com", Phone: "+6281234567890"},
		Items:     []connector.Item{{ReferenceID: "item-1", Type: "DIGITAL_PRODUCT", Name: "Item", NetUnitAmount: 1_000_000, Quantity: 1, Category: "software"}},
		ReturnURL: "https://emisell.example/return",
	})
	if err != nil || !strings.Contains(string(result.NextAction), "redirect") {
		t.Fatalf("unexpected result: %#v, %v", result, err)
	}
}

func TestOVOUsesMobileAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		properties, _ := body["channel_properties"].(map[string]any)
		if properties["account_mobile_number"] != "+6281234567890" {
			t.Fatalf("unexpected OVO payload: %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"pr-ovo","status":"REQUIRES_ACTION","channel_code":"OVO","actions":[]}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "order-ovo", LocalPaymentID: "pay_ovo",
		IdempotencyKey: "idem-ovo-1234", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "ewallet_ovo", ChannelCode: "OVO",
		Customer: connector.Customer{Name: "Budi", Phone: "+6281234567890"}, ReturnURL: "https://emisell.example/return",
	})
	if err != nil || !strings.Contains(string(result.NextAction), "mobile_authorization") {
		t.Fatalf("unexpected result: %#v, %v", result, err)
	}
}

func TestCardUsesHostedPaymentSessionWithoutPAN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sessions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		channels, _ := body["allowed_payment_channels"].([]any)
		if body["mode"] != "PAYMENT_LINK" || body["session_type"] != "PAY" || len(channels) != 1 || channels[0] != "CARDS" {
			t.Fatalf("unexpected card session payload: %#v", body)
		}
		encoded, _ := json.Marshal(body)
		if strings.Contains(string(encoded), "card_number") || strings.Contains(string(encoded), "cvv") || strings.Contains(string(encoded), "cvn") {
			t.Fatal("raw card data was sent by the connector")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"payment_session_id":"ps-661f87c614802d6c402cd82d","status":"ACTIVE","payment_link_url":"https://dev.xen.to/card-test"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "order-card", LocalPaymentID: "pay_card",
		IdempotencyKey: "idem-card-1234", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "card", ChannelCode: "CARDS",
		Customer: connector.Customer{Name: "Budi", Email: "budi@example.com", Phone: "+6281234567890"}, ReturnURL: "https://emisell.example/return",
	})
	if err != nil || result.ID != "ps-661f87c614802d6c402cd82d" || !strings.Contains(string(result.NextAction), "redirect") {
		t.Fatalf("unexpected hosted card session: %#v, %v", result, err)
	}
}

func TestGetHostedCardPaymentSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/sessions/ps-661f87c614802d6c402cd82d" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"payment_session_id":"ps-661f87c614802d6c402cd82d","status":"COMPLETED","payment_id":"py-card-1","payment_request_id":"pr-card-1"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	result, err := client.GetPayment(context.Background(), connector.PaymentLookup{Credentials: map[string]string{"api_key": "secret"}, PaymentID: "ps-661f87c614802d6c402cd82d"})
	if err != nil || result.Status != "SUCCEEDED" || result.ConnectorTransactionID != "py-card-1" {
		t.Fatalf("unexpected session lookup: %#v, %v", result, err)
	}
}

func TestXenditWebhookVerification(t *testing.T) {
	client, _ := New("https://api.xendit.test", time.Second)
	body := []byte(`{"event":"payment.capture","data":{"payment_request_id":"pr-123","status":"SUCCEEDED"}}`)
	event, err := client.HandleWebhook(context.Background(), connector.WebhookInput{
		Credentials: map[string]string{"webhook_verification_token": "callback-secret"},
		Headers:     http.Header{"X-Callback-Token": []string{"callback-secret"}, "Webhook-Id": []string{"evt-1"}}, Body: body,
	})
	if err != nil || event.ID != "evt-1" || event.PaymentID != "pr-123" || event.Status != "SUCCEEDED" {
		t.Fatalf("unexpected webhook event: %#v, %v", event, err)
	}
	if _, err = client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"webhook_verification_token": "callback-secret"}, Headers: http.Header{"X-Callback-Token": []string{"wrong"}}, Body: body}); err == nil {
		t.Fatal("invalid callback token was accepted")
	}
}

func TestXenditPaymentSessionWebhook(t *testing.T) {
	client, _ := New("https://api.xendit.test", time.Second)
	body := []byte(`{"event":"payment_session.completed","data":{"payment_session_id":"ps-card-1","payment_request_id":"pr-card-1","status":"COMPLETED"}}`)
	event, err := client.HandleWebhook(context.Background(), connector.WebhookInput{
		Credentials: map[string]string{"webhook_verification_token": "callback-secret"},
		Headers:     http.Header{"X-Callback-Token": []string{"callback-secret"}, "Webhook-Id": []string{"evt-session-1"}}, Body: body,
	})
	if err != nil || event.PaymentID != "ps-card-1" || event.Status != "COMPLETED" {
		t.Fatalf("unexpected payment session webhook: %#v, %v", event, err)
	}
}

func TestProviderAmount(t *testing.T) {
	value, err := providerAmount(1_000_000, "IDR")
	if err != nil || value != int64(10_000) {
		t.Fatalf("unexpected amount conversion: %#v, %v", value, err)
	}
	if _, err = providerAmount(1_000_001, "IDR"); err == nil {
		t.Fatal("fractional rupiah was accepted")
	}
}

func TestMutationTimeoutIsUnknownButReadTimeoutIsNot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()
	client, _ := New(server.URL, 20*time.Millisecond)
	_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "timeout-create",
		IdempotencyKey: "timeout-create-123", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "qris", ChannelCode: "QRIS",
	})
	if !errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("mutation timeout must be UNKNOWN, got %v", err)
	}
	_, err = client.GetPayment(context.Background(), connector.PaymentLookup{Credentials: map[string]string{"api_key": "secret"}, PaymentID: "pr-timeout"})
	if err == nil || errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("read timeout must remain retryable without UNKNOWN mutation state, got %v", err)
	}
}

func TestMutationServerFailureAndMalformedSuccessAreUnknown(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `{"error_code":"SERVER_ERROR"}`},
		{name: "request timeout", status: http.StatusRequestTimeout, body: `{"error_code":"REQUEST_TIMEOUT"}`},
		{name: "malformed success", status: http.StatusCreated, body: `{"id":`},
		{name: "missing identity", status: http.StatusCreated, body: `{"status":"PENDING"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, _ := New(server.URL, time.Second)
			_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
				Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "ambiguous-create",
				IdempotencyKey: "ambiguous-create-123", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "qris", ChannelCode: "QRIS",
			})
			if !errors.Is(err, connector.ErrOutcomeUnknown) {
				t.Fatalf("ambiguous mutation response must be UNKNOWN, got %v", err)
			}
		})
	}
}

func TestMutationClientRejectionIsDeterministic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error_code":"API_VALIDATION_ERROR","message":"invalid channel"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, time.Second)
	_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Credentials: map[string]string{"api_key": "secret"}, MerchantReference: "rejected-create",
		IdempotencyKey: "rejected-create-123", Amount: 1_000_000, Currency: "IDR", PaymentMethodCode: "qris", ChannelCode: "QRIS",
	})
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "API_VALIDATION_ERROR" || errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("provider 4xx must remain a deterministic rejection, got %v", err)
	}
}

func TestManifestConformance(t *testing.T) {
	client, err := New("https://api.xendit.test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	manifest := client.Manifest()
	if err = manifest.Validate(); err != nil {
		t.Fatalf("invalid connector manifest: %v", err)
	}
	if manifest.Code != client.Code() || !manifest.Supports(connector.OperationCreatePayment) || !manifest.Supports(connector.OperationHandleWebhook) {
		t.Fatalf("unexpected manifest capabilities: %#v", manifest)
	}
	if manifest.Supports(connector.OperationCreateRefund) {
		t.Fatal("refund must not be advertised before its connector implementation is certified")
	}
	profile, ok := manifest.CertificationProfile("card")
	if !ok || profile.Code != "xendit-payment-session/card" || !profile.Automated {
		t.Fatalf("unexpected card certification profile: %#v", profile)
	}
}

func TestConnectorPaymentMethodMapping(t *testing.T) {
	client, _ := New("https://api.xendit.test", time.Second)
	tests := []connector.PaymentMethodMapping{
		{PaymentMethodCode: "qris", ProviderMethod: "qr_code", ProviderMethodType: "qris"},
		{PaymentMethodCode: "qris", ProviderMethod: "real_time_payment", ProviderMethodType: "qris"},
		{PaymentMethodCode: "va_bca", ProviderMethod: "bank_transfer", ProviderMethodType: "bca"},
		{PaymentMethodCode: "ewallet_dana", ProviderMethod: "wallet", ProviderMethodType: "dana"},
		{PaymentMethodCode: "card", ProviderMethod: "card", ProviderMethodType: "card"},
	}
	for _, input := range tests {
		if err := client.ValidatePaymentMethod(input); err != nil {
			t.Fatalf("valid mapping was rejected: %#v: %v", input, err)
		}
	}
	if err := client.ValidatePaymentMethod(connector.PaymentMethodMapping{PaymentMethodCode: "va_bca", ProviderMethod: "wallet", ProviderMethodType: "bca"}); err == nil {
		t.Fatal("invalid provider mapping was accepted")
	}
	if err := client.ValidatePaymentMethod(connector.PaymentMethodMapping{PaymentMethodCode: "unknown", ProviderMethod: "wallet", ProviderMethodType: "test"}); err == nil {
		t.Fatal("unknown canonical method was accepted")
	}
}

func TestConnectorPaymentAmountValidation(t *testing.T) {
	client, _ := New("https://api.xendit.test", time.Second)
	tests := []struct {
		name      string
		input     connector.PaymentValidation
		wantError bool
	}{
		{name: "valid QRIS", input: connector.PaymentValidation{PaymentMethodCode: "qris", Currency: "IDR", Amount: 1_000_000}},
		{name: "IDR fraction", input: connector.PaymentValidation{PaymentMethodCode: "qris", Currency: "IDR", Amount: 1_000_001}, wantError: true},
		{name: "below QRIS minimum", input: connector.PaymentValidation{PaymentMethodCode: "qris", Currency: "IDR", Amount: 99}, wantError: true},
		{name: "above QRIS maximum", input: connector.PaymentValidation{PaymentMethodCode: "qris", Currency: "IDR", Amount: 1_000_000_100}, wantError: true},
		{name: "valid BCA VA", input: connector.PaymentValidation{PaymentMethodCode: "va_bca", Currency: "IDR", Amount: 1_000_000}},
		{name: "below BRI VA minimum", input: connector.PaymentValidation{PaymentMethodCode: "va_bri", Currency: "IDR", Amount: 999_900}, wantError: true},
		{name: "valid card", input: connector.PaymentValidation{PaymentMethodCode: "card", Currency: "IDR", Amount: 1_000_000}},
		{name: "below card minimum", input: connector.PaymentValidation{PaymentMethodCode: "card", Currency: "IDR", Amount: 499_900}, wantError: true},
		{name: "unsupported method", input: connector.PaymentValidation{PaymentMethodCode: "cash", Currency: "IDR", Amount: 1_000_000}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := client.ValidatePayment(test.input)
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestProviderErrorMessageIncludesValidationDetail(t *testing.T) {
	message := providerErrorMessage(map[string]any{
		"message": "API Validation Error",
		"errors":  []any{map[string]any{"message": "Channel not supported yet."}},
	})
	if message != "API Validation Error; Channel not supported yet." {
		t.Fatalf("unexpected provider error message %q", message)
	}
}
