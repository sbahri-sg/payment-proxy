package midtrans

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
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

func TestMidtransInstallationAndQRISPayment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "SB-Mid-server-test" || password != "" {
			t.Fatal("Midtrans Server Key was not sent with Basic authentication")
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"status_code":"404","status_message":"Transaction doesn't exist."}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v2/charge":
			if r.Header.Get("X-Override-Notification") != "https://payments.example.com/webhooks/v1/providers/midtrans/ins_1" {
				t.Fatalf("unexpected notification override: %q", r.Header.Get("X-Override-Notification"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			details, _ := body["transaction_details"].(map[string]any)
			qris, _ := body["qris"].(map[string]any)
			if body["payment_type"] != "qris" || details["gross_amount"] != float64(10000) || qris["acquirer"] != "gopay" {
				t.Fatalf("unexpected QRIS payload: %#v", body)
			}
			_, _ = io.WriteString(w, `{"status_code":"201","transaction_id":"mid-trx-1","order_id":"pay_1","transaction_status":"pending","actions":[{"name":"generate-qr-code","url":"https://api.sandbox.midtrans.com/qr-code/test"}]}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, err := New(server.URL, server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	installation, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_1", ProviderCode: "midtrans", Environment: "sandbox",
		Credentials:      map[string]string{"server_key": "SB-Mid-server-test"},
		PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/midtrans/ins_1",
	})
	if err != nil || installation.ConnectorID != "midtrans:ins_1" || installation.Environment != "sandbox" || !installation.WebhookReady {
		t.Fatalf("unexpected installation: %#v, %v", installation, err)
	}
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		ProviderCode: "midtrans", Environment: "sandbox", Credentials: map[string]string{"server_key": "SB-Mid-server-test"},
		LocalPaymentID: "pay_1", MerchantReference: "order-1", IdempotencyKey: "idem-1",
		Amount: 10_000, Currency: "IDR", PaymentMethodCode: "qris",
		PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/midtrans/ins_1",
	})
	if err != nil || result.ID != "pay_1" || result.Status != "PENDING" || !strings.Contains(string(result.NextAction), "qr_code_url") {
		t.Fatalf("unexpected payment: %#v, %v", result, err)
	}
}

func TestMidtransCredentialEnvironment(t *testing.T) {
	for key, expected := range map[string]string{
		"SB-Mid-server-example": "sandbox",
		"Mid-server-example":    "live",
	} {
		actual, err := midtransEnvironment(key)
		if err != nil || actual != expected {
			t.Fatalf("key %q: environment=%q err=%v", key, actual, err)
		}
	}
	if _, err := midtransEnvironment("unknown-key"); !errors.Is(err, connector.ErrInvalidCredential) {
		t.Fatalf("unknown Midtrans key was not rejected: %v", err)
	}
}

func TestProviderHostedCheckoutRestrictsMidtransSnapToActiveMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/snap/v1/transactions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Override-Notification") != "https://payments.example.com/webhooks/v1/providers/midtrans/ins_1" {
			t.Fatalf("unexpected notification override: %q", r.Header.Get("X-Override-Notification"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		callbacks, _ := body["callbacks"].(map[string]any)
		enabled, _ := body["enabled_payments"].([]any)
		if body["payment_type"] != nil || len(enabled) != 2 || enabled[0] != "other_qris" || enabled[1] != "bca_va" || body["gopay"] != nil || body["shopeepay"] != nil || callbacks["finish"] != "https://shop.example/payments/return" {
			t.Fatalf("Snap checkout must receive the exact active method allowlist: %#v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"token":"snap-token-1","redirect_url":"https://app.sandbox.midtrans.com/snap/v3/redirection/test"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "SB-Mid-server-test"},
		CheckoutMode: connector.CheckoutModeProviderHosted, LocalPaymentID: "pay_hosted_1", MerchantReference: "order-hosted",
		Amount: 10_000, Currency: "IDR", ReturnURL: "https://shop.example/payments/return",
		PublicWebhookURL: "https://payments.example.com/webhooks/v1/providers/midtrans/ins_1",
		AllowedPaymentMethods: []connector.PaymentMethodMapping{
			{PaymentMethodCode: "qris", ProviderMethod: "real_time_payment", ProviderMethodType: "qris", ProviderChannelCode: "other_qris"},
			{PaymentMethodCode: "va_bca", ProviderMethod: "bank_transfer", ProviderMethodType: "bca", ProviderChannelCode: "bca_va"},
		},
	})
	if err != nil || result.ID != "pay_hosted_1" || result.Status != "REQUIRES_ACTION" || !strings.Contains(string(result.NextAction), "app.sandbox.midtrans.com") {
		t.Fatalf("unexpected Snap checkout: %#v, %v", result, err)
	}
}

func TestOfficialMidtransCoreURLsResolveToSnapHosts(t *testing.T) {
	client, err := New("https://api.sandbox.midtrans.com", "https://api.midtrans.com", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if client.sandboxSnapURL.Host != "app.sandbox.midtrans.com" || client.liveSnapURL.Host != "app.midtrans.com" {
		t.Fatalf("unexpected Snap hosts: %s %s", client.sandboxSnapURL, client.liveSnapURL)
	}
}

func TestMidtransVirtualAccountAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/charge":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bank, _ := body["bank_transfer"].(map[string]any)
			if body["payment_type"] != "bank_transfer" || bank["bank"] != "bca" {
				t.Fatalf("unexpected VA payload: %#v", body)
			}
			_, _ = io.WriteString(w, `{"transaction_id":"mid-va-1","order_id":"pay_va_1","transaction_status":"pending","va_numbers":[{"bank":"bca","va_number":"1234567890"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/pay_va_1/status":
			_, _ = io.WriteString(w, `{"transaction_id":"mid-va-1","order_id":"pay_va_1","transaction_status":"settlement","va_numbers":[{"bank":"bca","va_number":"1234567890"}]}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	created, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, LocalPaymentID: "pay_va_1",
		Amount: 10_000, Currency: "IDR", PaymentMethodCode: "va_bca",
	})
	if err != nil || !strings.Contains(string(created.NextAction), "1234567890") {
		t.Fatalf("unexpected VA result: %#v, %v", created, err)
	}
	status, err := client.GetPayment(context.Background(), connector.PaymentLookup{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, PaymentID: created.ID,
	})
	if err != nil || status.Status != "SUCCEEDED" {
		t.Fatalf("unexpected status: %#v, %v", status, err)
	}
}

func TestMidtransPermataUsesDedicatedPaymentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["payment_type"] != "permata" || body["bank_transfer"] != nil {
			t.Fatalf("unexpected Permata payload: %#v", body)
		}
		_, _ = io.WriteString(w, `{"transaction_id":"mid-permata-1","order_id":"pay_permata_1","transaction_status":"pending","permata_va_number":"1234567890"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	created, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, LocalPaymentID: "pay_permata_1",
		Amount: 10_000, Currency: "IDR", PaymentMethodCode: "va_permata",
	})
	if err != nil || !strings.Contains(string(created.NextAction), "1234567890") {
		t.Fatalf("unexpected Permata result: %#v, %v", created, err)
	}
}

func TestMidtransRejectsMethodsNotImplementedByCoreV2(t *testing.T) {
	client, _ := New("https://api.sandbox.midtrans.test", "https://api.midtrans.test", time.Second)
	for _, code := range []string{"va_danamon", "va_bsi", "card"} {
		err := client.ValidatePayment(connector.PaymentValidation{PaymentMethodCode: code, Amount: 10_000, Currency: "IDR"})
		if err == nil {
			t.Fatalf("method %s must remain unavailable until its own implementation exists", code)
		}
	}
}

func TestMidtransManifestKeepsUncertifiedMutationsClosed(t *testing.T) {
	client, _ := New("https://api.sandbox.midtrans.test", "https://api.midtrans.test", time.Second)
	manifest := client.Manifest()
	for _, operation := range []connector.Operation{
		connector.OperationCancelPayment,
		connector.OperationCreateRefund,
		connector.OperationGetRefund,
	} {
		if manifest.Supports(operation) {
			t.Fatalf("operation %s must not be advertised before real sandbox evidence exists", operation)
		}
	}
}

func TestMidtransCancelAndRefund(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/pay_1/cancel":
			_, _ = io.WriteString(w, `{"transaction_id":"mid-1","order_id":"pay_1","transaction_status":"cancel"}`)
		case "/v2/pay_1/refund":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["refund_key"] != "refund-1" || body["amount"] != float64(500) {
				t.Fatalf("unexpected refund payload: %#v", body)
			}
			_, _ = io.WriteString(w, `{"order_id":"pay_1","transaction_status":"refund","refund_key":"refund-1"}`)
		case "/v2/pay_1/status":
			_, _ = io.WriteString(w, `{"order_id":"pay_1","transaction_status":"refund","refunds":[{"refund_key":"refund-1","refund_status":"success","bank_confirmed_at":"2026-08-31 10:00:00"}]}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	lookup := connector.PaymentLookup{Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, PaymentID: "pay_1"}
	cancelled, err := client.CancelPayment(context.Background(), lookup, "cancel-1", "customer")
	if err != nil || cancelled.Status != "CANCELLED" {
		t.Fatalf("unexpected cancellation: %#v, %v", cancelled, err)
	}
	refunded, err := client.CreateRefund(context.Background(), connector.RefundInput{
		Environment: "sandbox", Credentials: lookup.Credentials, PaymentID: "pay_1", IdempotencyKey: "refund-1",
		Amount: 500, Currency: "IDR", Reason: "customer request",
	})
	if err != nil || refunded.Status != "PENDING" || refunded.ID != "pay_1|refund-1" {
		t.Fatalf("unexpected refund: %#v, %v", refunded, err)
	}
	status, err := client.GetRefund(context.Background(), connector.RefundLookup{
		Environment: "sandbox", Credentials: lookup.Credentials, RefundID: refunded.ID,
	})
	if err != nil || status.Status != "SUCCEEDED" {
		t.Fatalf("unexpected refund status: %#v, %v", status, err)
	}
}

func TestMidtransWebhookSignature(t *testing.T) {
	key := "SB-Mid-server-test"
	orderID, statusCode, grossAmount := "pay_1", "200", "10000.00"
	digest := sha512.Sum512([]byte(orderID + statusCode + grossAmount + key))
	body := []byte(`{"transaction_id":"mid-1","order_id":"pay_1","status_code":"200","gross_amount":"10000.00","transaction_status":"settlement","signature_key":"` + hex.EncodeToString(digest[:]) + `"}`)
	client, _ := New("https://api.sandbox.midtrans.test", "https://api.midtrans.test", time.Second)
	event, err := client.HandleWebhook(context.Background(), connector.WebhookInput{
		ProviderCode: "midtrans", Credentials: map[string]string{"server_key": key}, Body: body,
	})
	if err != nil || event.PaymentID != orderID || event.Status != "SUCCEEDED" || event.Type != "payment.updated" {
		t.Fatalf("unexpected webhook: %#v, %v", event, err)
	}
	body = []byte(strings.Replace(string(body), hex.EncodeToString(digest[:]), "invalid", 1))
	if _, err = client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"server_key": key}, Body: body}); err == nil {
		t.Fatal("invalid Midtrans signature was accepted")
	}
}

func TestMidtransRefundWebhookWaitsForBankConfirmation(t *testing.T) {
	key := "SB-Mid-server-test"
	orderID, statusCode, grossAmount := "pay_1", "200", "10000.00"
	digest := sha512.Sum512([]byte(orderID + statusCode + grossAmount + key))
	signature := hex.EncodeToString(digest[:])
	client, _ := New("https://api.sandbox.midtrans.test", "https://api.midtrans.test", time.Second)

	pendingBody := []byte(`{"transaction_id":"mid-1","order_id":"pay_1","status_code":"200","gross_amount":"10000.00","transaction_status":"refund","signature_key":"` + signature + `","refunds":[{"refund_key":"refund-1","refund_status":"success"}]}`)
	pending, err := client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"server_key": key}, Body: pendingBody})
	if err != nil || pending.Type != "refund.updated" || pending.RefundID != "pay_1|refund-1" || pending.Status != "PENDING" {
		t.Fatalf("unexpected pending refund webhook: %#v, %v", pending, err)
	}

	confirmedBody := []byte(`{"transaction_id":"mid-1","order_id":"pay_1","status_code":"200","gross_amount":"10000.00","transaction_status":"refund","signature_key":"` + signature + `","refunds":[{"refund_key":"refund-1","refund_status":"success","bank_confirmed_at":"2026-08-31 10:00:00"}]}`)
	confirmed, err := client.HandleWebhook(context.Background(), connector.WebhookInput{Credentials: map[string]string{"server_key": key}, Body: confirmedBody})
	if err != nil || confirmed.Status != "SUCCEEDED" || confirmed.ID == pending.ID {
		t.Fatalf("unexpected confirmed refund webhook: %#v, %v", confirmed, err)
	}
}

func TestMidtransMutationServerErrorIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"status_code":"503","status_message":"temporary"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, LocalPaymentID: "pay_1",
		Amount: 10_000, Currency: "IDR", PaymentMethodCode: "qris",
	})
	if !errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("server ambiguity was not preserved: %v", err)
	}
}

func TestMidtransHTTP200WithBusinessErrorIsRejected(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status_code":"404","status_message":"Merchant pop id is not found"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, LocalPaymentID: "pay_1",
		Amount: 10_000, Currency: "IDR", PaymentMethodCode: "qris",
	})
	var apiErr *connector.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound || apiErr.Code != "MIDTRANS_POP_ID_REQUIRED" || errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("HTTP 200 Midtrans business error was not rejected deterministically: %v", err)
	}
}

func TestMidtransSendsOptionalPoPID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-POP-ID") != "merchant-pop-id" {
			t.Fatalf("expected optional PoP ID header, got %q", r.Header.Get("X-POP-ID"))
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"status_code":"404","status_message":"Transaction doesn't exist."}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	result, err := client.VerifyInstallation(context.Background(), connector.InstallationInput{
		InstallationID: "ins_pop", Environment: "sandbox",
		Credentials: map[string]string{"server_key": "SB-Mid-server-example", "pop_id": "merchant-pop-id"},
	})
	if err != nil || result.StoredCredentials["pop_id"] != "merchant-pop-id" {
		t.Fatalf("unexpected PoP credential result: %#v, err=%v", result, err)
	}
}

func TestMidtransHTTP200WithBusinessServerErrorIsUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status_code":"500","status_message":"temporary provider failure"}`)
	}))
	defer server.Close()
	client, _ := New(server.URL, server.URL, time.Second)
	_, err := client.CreatePayment(context.Background(), connector.PaymentInput{
		Environment: "sandbox", Credentials: map[string]string{"server_key": "server"}, LocalPaymentID: "pay_1",
		Amount: 10_000, Currency: "IDR", PaymentMethodCode: "qris",
	})
	if !errors.Is(err, connector.ErrOutcomeUnknown) {
		t.Fatalf("HTTP 200 Midtrans business 5xx did not preserve ambiguity: %v", err)
	}
}
