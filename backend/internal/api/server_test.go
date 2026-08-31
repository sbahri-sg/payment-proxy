package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/observability"
	"github.com/emisell/api-payment-proxy/internal/ratelimit"
	"github.com/go-chi/chi/v5/middleware"
)

func TestXenditCredentialMetadataContainsNoSecret(t *testing.T) {
	input := map[string]string{"api_key": "xnd_development_12345678"}
	schema := json.RawMessage(`[{"code":"api_key","secret":true,"required":true}]`)
	metadata, err := validateAndMaskCredentials(schema, input)
	if err != nil {
		t.Fatal(err)
	}
	if contains(string(metadata), input["api_key"]) {
		t.Fatal("credential leaked into metadata")
	}
	if contains(string(metadata), "last_four") || contains(string(metadata), "5678") {
		t.Fatal("secret fragment leaked into metadata")
	}
}

func TestPaymentStatusMapping(t *testing.T) {
	cases := map[string]string{"succeeded": "SUCCEEDED", "requires_customer_action": "PENDING", "failed": "FAILED", "unrecognized": "UNKNOWN"}
	for input, expected := range cases {
		if got := mapPaymentStatus(input); got != expected {
			t.Fatalf("%s: got %s, want %s", input, got, expected)
		}
	}
}

func TestActorIsDerivedByServerAndIgnoresRequestHeader(t *testing.T) {
	serviceRequest := httptest.NewRequest(http.MethodPost, "/api/v1/payment-sessions", nil)
	serviceRequest.Header.Set("X-Emisell-Actor", "forged-operator")
	if got := actor(serviceRequest); got != "emisell-backend" {
		t.Fatalf("service actor = %q, want emisell-backend", got)
	}

	adminRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/service-api-keys", nil)
	adminRequest.Header.Set("X-Emisell-Actor", "forged-operator")
	if got := actor(adminRequest); got != "payment-proxy-admin" {
		t.Fatalf("admin actor = %q, want payment-proxy-admin", got)
	}
}

func TestCanonicalAdminRouteUsesAdminAuthentication(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := New(config.Config{AdminAPIKey: "test-admin-key"}, nil, nil, nil, nil, nil, logger)

	canonical := httptest.NewRecorder()
	handler.ServeHTTP(canonical, httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-app-providers", nil))
	if canonical.Code != http.StatusUnauthorized || !strings.Contains(canonical.Body.String(), "invalid admin credential") {
		t.Fatalf("canonical admin route used the wrong authentication chain: %d %s", canonical.Code, canonical.Body.String())
	}

	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, httptest.NewRequest(http.MethodGet, "/internal/v1/provider-app-providers", nil))
	if legacy.Code != http.StatusUnauthorized || legacy.Header().Get("Deprecation") != "true" {
		t.Fatalf("legacy admin compatibility route is not marked deprecated: %d %#v", legacy.Code, legacy.Header())
	}
}

func TestBearerTokenRequiresExactBearerScheme(t *testing.T) {
	if got := bearerToken("Bearer epk_example"); got != "epk_example" {
		t.Fatalf("valid bearer token was not parsed: %q", got)
	}
	for _, input := range []string{"epk_example", "Basic epk_example", "Bearer", "Bearer first second"} {
		if got := bearerToken(input); got != "" {
			t.Fatalf("invalid authorization value %q returned token %q", input, got)
		}
	}
}

func TestPaymentListQueryValidation(t *testing.T) {
	if !validPaymentStatus("UNKNOWN") || validPaymentStatus("RETRYING") {
		t.Fatal("payment status allowlist is incorrect")
	}
	if value, err := queryInt("", 25, 1, 100); err != nil || value != 25 {
		t.Fatalf("default query value was not applied: %d, %v", value, err)
	}
	if _, err := queryInt("101", 25, 1, 100); err == nil {
		t.Fatal("out-of-range limit was accepted")
	}
}

func TestWebhookStatusAllowlists(t *testing.T) {
	if !validWebhookInboxStatus("PROCESSED") || validWebhookInboxStatus("DELIVERED") {
		t.Fatal("webhook inbox status allowlist is incorrect")
	}
	if !validWebhookDeliveryStatus("DEAD") || validWebhookDeliveryStatus("FAILED") {
		t.Fatal("webhook delivery status allowlist is incorrect")
	}
}

func TestReconciliationKindAllowlist(t *testing.T) {
	if !validReconciliationKind("PAYMENT_UNKNOWN") || !validReconciliationKind("DELIVERY_DEAD") {
		t.Fatal("supported reconciliation kinds were rejected")
	}
	if validReconciliationKind("DELETE_PAYMENT") {
		t.Fatal("unsupported reconciliation kind was accepted")
	}
}

func TestManualCertificationAction(t *testing.T) {
	err := &connector.APIError{Provider: "xendit", Status: 400, Code: "PAYMENT_METHOD_NOT_SUPPORTED"}
	if !isManualCertificationAction(err, json.RawMessage(`{"type":"redirect","url":"https://example.test/pay"}`)) {
		t.Fatal("redirect customer authorization should be resumable")
	}
	if isManualCertificationAction(err, json.RawMessage(`{"type":"qr_code"}`)) {
		t.Fatal("QR simulator failure should not be treated as manual authorization")
	}
	if isManualCertificationAction(&connector.APIError{Provider: "xendit", Status: 400, Code: "API_VALIDATION_ERROR"}, json.RawMessage(`{"type":"redirect"}`)) {
		t.Fatal("validation errors should remain failed")
	}
	if !isManualCertificationAction(connector.ErrNotSupported, json.RawMessage(`{"type":"redirect"}`)) {
		t.Fatal("hosted customer authorization should be resumable when the provider has no simulator")
	}
	for _, actionType := range []string{"qr_code_information", "virtual_account_information", "provider_actions"} {
		if !isManualCertificationAction(connector.ErrNotSupported, json.RawMessage(`{"type":"`+actionType+`"}`)) {
			t.Fatalf("%s customer instruction should be resumable when the connector has no simulator", actionType)
		}
	}
}

func TestPaymentOptionContract(t *testing.T) {
	if got := defaultPaymentOptionLabel("virtual_account"); got != "VIRTUAL ACCOUNT" {
		t.Fatalf("unexpected default label %q", got)
	}
	if !paymentMethodPattern.MatchString("real_time_payment") || paymentMethodPattern.MatchString("QRIS / Xendit") {
		t.Fatal("payment method identifier validation is incorrect")
	}
}

func TestAccessLogAndMetricsDoNotLeakCredentials(t *testing.T) {
	var logs bytes.Buffer
	metrics := observability.New()
	server := &Server{metrics: metrics, log: slog.New(slog.NewJSONHandler(&logs, nil))}
	handler := server.accessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/payment-sessions?api_key=query-secret", strings.NewReader(`{"api_key":"body-secret"}`))
	request.Header.Set("Authorization", "Bearer authorization-secret")
	request.Header.Set("X-Admin-API-Key", "admin-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	output := logs.String()
	for _, secret := range []string{"query-secret", "body-secret", "authorization-secret", "admin-secret"} {
		if strings.Contains(output, secret) {
			t.Fatalf("access log leaked %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, `"status":202`) || !strings.Contains(output, `"path":"/api/v1/payment-sessions"`) {
		t.Fatalf("access log is missing safe operational fields: %s", output)
	}
	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != 1 || snapshot.Responses.Status2xx != 1 || snapshot.InFlight != 0 {
		t.Fatalf("request metrics were not recorded: %#v", snapshot)
	}
	panicHandler := server.accessLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("test panic") }))
	func() {
		defer func() { _ = recover() }()
		panicHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()
	snapshot = metrics.Snapshot()
	if snapshot.RequestsTotal != 2 || snapshot.Responses.Status5xx != 1 || snapshot.InFlight != 0 {
		t.Fatalf("panic request leaked in-flight metrics: %#v", snapshot)
	}
}

func TestAPIContractVersionNegotiationAndHeaders(t *testing.T) {
	server := &Server{}
	handler := middleware.RequestID(server.contractHeaders(server.requireAPIVersion(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))

	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, httptest.NewRequest(http.MethodGet, "/api/v1/engine/capabilities", nil))
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("request without pinned version was rejected: %d", accepted.Code)
	}
	if accepted.Header().Get(apiContractHeader) != apiContractVersion || accepted.Header().Get("X-Request-ID") == "" {
		t.Fatalf("contract response headers are incomplete: %#v", accepted.Header())
	}

	rejectedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/engine/capabilities", nil)
	rejectedRequest.Header.Set(apiContractHeader, "2025-01-01")
	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, rejectedRequest)
	if rejected.Code != http.StatusNotAcceptable || !strings.Contains(rejected.Body.String(), "UNSUPPORTED_API_VERSION") {
		t.Fatalf("unsupported contract version was accepted: %d %s", rejected.Code, rejected.Body.String())
	}
}

func TestServiceTrafficGuards(t *testing.T) {
	server := &Server{rateLimiter: ratelimit.New(1, 1)}
	handler := server.protectServiceTraffic(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func() *http.Request {
		item := httptest.NewRequest(http.MethodGet, "/api/v1/payment-options", nil)
		item.Header.Set("X-Emisell-Merchant-ID", "merchant-a")
		return item
	}
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request())
	if first.Code != http.StatusNoContent || first.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("first request did not consume the configured burst: %d %#v", first.Code, first.Header())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, request())
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("rate limit did not reject excess request: %d %#v", second.Code, second.Header())
	}

	busyServer := &Server{inFlight: make(chan struct{}, 1)}
	busyServer.inFlight <- struct{}{}
	busy := httptest.NewRecorder()
	busyServer.protectServiceTraffic(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("busy handler should not run")
	})).ServeHTTP(busy, request())
	if busy.Code != http.StatusServiceUnavailable || !strings.Contains(busy.Body.String(), "API_BUSY") {
		t.Fatalf("concurrency guard did not reject excess request: %d %s", busy.Code, busy.Body.String())
	}
}

func TestAdminTrafficGuardUsesClientIP(t *testing.T) {
	server := &Server{adminRateLimiter: ratelimit.New(1, 1)}
	handler := server.protectAdminTraffic(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := func(remoteAddr string) *http.Request {
		item := httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-app-providers", nil)
		item.RemoteAddr = remoteAddr
		return item
	}

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request("203.0.113.10:41000"))
	if first.Code != http.StatusNoContent || first.Header().Get("X-Emisell-RateLimit-Scope") != "replica-admin-ip" {
		t.Fatalf("first admin request was not accepted: %d %#v", first.Code, first.Header())
	}
	limited := httptest.NewRecorder()
	handler.ServeHTTP(limited, request("203.0.113.10:41001"))
	if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") == "" {
		t.Fatalf("admin rate limit did not reject repeated client IP: %d %#v", limited.Code, limited.Header())
	}
	otherClient := httptest.NewRecorder()
	handler.ServeHTTP(otherClient, request("203.0.113.11:41000"))
	if otherClient.Code != http.StatusNoContent {
		t.Fatalf("independent admin client was incorrectly limited: %d", otherClient.Code)
	}
}

func TestRequestDeadlineIsPropagated(t *testing.T) {
	server := &Server{cfg: config.Config{APIRequestTimeout: time.Second}}
	seen := false
	server.requestDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, seen = r.Context().Deadline()
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/providers", nil))
	if !seen {
		t.Fatal("service request did not receive a processing deadline")
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
