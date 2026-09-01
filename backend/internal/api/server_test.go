package api

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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

func TestValidateProviderLogo(t *testing.T) {
	var valid bytes.Buffer
	if err := png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	if contentType, err := validateProviderLogo(valid.Bytes(), "image/png"); err != nil || contentType != "image/png" {
		t.Fatalf("valid PNG rejected: %q, %v", contentType, err)
	}
	if _, err := validateProviderLogo(valid.Bytes(), "image/jpeg"); err == nil {
		t.Fatal("mismatched declared content type was accepted")
	}
	if _, err := validateProviderLogo([]byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "image/svg+xml"); err == nil {
		t.Fatal("SVG logo was accepted")
	}

	var oversizedDimension bytes.Buffer
	large := image.NewRGBA(image.Rect(0, 0, maxProviderLogoSide+1, 1))
	large.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&oversizedDimension, large); err != nil {
		t.Fatal(err)
	}
	if _, err := validateProviderLogo(oversizedDimension.Bytes(), "image/png"); err == nil {
		t.Fatal("oversized logo dimensions were accepted")
	}
}

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

func TestCredentialClearFieldsMustBelongToProviderSchema(t *testing.T) {
	schema := json.RawMessage(`[{"code":"api_key","secret":true,"required":true},{"code":"webhook_token","secret":true,"required":false}]`)
	if err := validateCredentialFieldNames(schema, []string{"webhook_token"}); err != nil {
		t.Fatalf("valid optional field was rejected: %v", err)
	}
	if err := validateCredentialFieldNames(schema, []string{"admin_override"}); err == nil {
		t.Fatal("unsupported clear field was accepted")
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

func TestRefundGovernanceNormalization(t *testing.T) {
	for input, expected := range map[string]string{
		"requested_by_customer": "REQUESTED_BY_CUSTOMER",
		"customer_request":      "REQUESTED_BY_CUSTOMER",
		"order-cancelled":       "CANCELLATION",
		"other":                 "OTHERS",
	} {
		got, ok := normalizeRefundReason(input)
		if !ok || got != expected {
			t.Fatalf("%q normalized to %q, %v; want %q", input, got, ok, expected)
		}
	}
	if _, ok := normalizeRefundReason("send_to_another_bank"); ok {
		t.Fatal("an ungoverned refund reason was accepted")
	}
	policy, err := refundPolicyFromMetadata(json.RawMessage(`{"refund":{"supported":true,"partial":false,"multiple_partial":false,"return_to_original_source":true,"confirmation":"WEBHOOK","window_days":30}}`))
	if err != nil || !policy.Supported || policy.Partial || !policy.ReturnToOriginalSource || policy.Confirmation != "webhook" || policy.WindowDays != 30 {
		t.Fatalf("unexpected refund policy: %#v, %v", policy, err)
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

func TestAdminRoutesUseCanonicalNamespace(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := New(config.Config{AdminAPIKey: "test-admin-key", ServiceAPIKey: "test-service-key"}, nil, nil, nil, nil, nil, logger)

	canonical := httptest.NewRecorder()
	handler.ServeHTTP(canonical, httptest.NewRequest(http.MethodGet, "/api/v1/admin/provider-app-providers", nil))
	if canonical.Code != http.StatusUnauthorized || !strings.Contains(canonical.Body.String(), "invalid admin credential") {
		t.Fatalf("canonical admin route used the wrong authentication chain: %d %s", canonical.Code, canonical.Body.String())
	}

	legacy := httptest.NewRecorder()
	legacyRequest := httptest.NewRequest(http.MethodGet, "/internal/v1/provider-app-providers", nil)
	legacyRequest.Header.Set("X-Admin-API-Key", "test-admin-key")
	handler.ServeHTTP(legacy, legacyRequest)
	if legacy.Code != http.StatusNotFound {
		t.Fatalf("removed legacy admin namespace returned %d, want 404", legacy.Code)
	}

	genericUpload := httptest.NewRecorder()
	genericUploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/provider-apps", nil)
	genericUploadRequest.Header.Set("X-Admin-API-Key", "test-admin-key")
	handler.ServeHTTP(genericUpload, genericUploadRequest)
	if genericUpload.Code != http.StatusMethodNotAllowed {
		t.Fatalf("removed generic Provider App upload returned %d, want 405", genericUpload.Code)
	}

	internalDiagnostic := httptest.NewRecorder()
	internalDiagnosticRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/connector-certifications", nil)
	internalDiagnosticRequest.Header.Set("X-Admin-API-Key", "test-admin-key")
	handler.ServeHTTP(internalDiagnostic, internalDiagnosticRequest)
	if internalDiagnostic.Code != http.StatusBadRequest || !strings.Contains(internalDiagnostic.Body.String(), "INVALID_TENANT") {
		t.Fatalf("internal diagnostic route did not require an explicit test tenant: %d %s", internalDiagnostic.Code, internalDiagnostic.Body.String())
	}

	removedServiceDiagnostic := httptest.NewRecorder()
	removedServiceDiagnosticRequest := httptest.NewRequest(http.MethodGet, "/api/v1/connector-certifications", nil)
	removedServiceDiagnosticRequest.Header.Set("Authorization", "Bearer test-service-key")
	removedServiceDiagnosticRequest.Header.Set("X-Emisell-Merchant-ID", "merchant_test")
	handler.ServeHTTP(removedServiceDiagnostic, removedServiceDiagnosticRequest)
	if removedServiceDiagnostic.Code != http.StatusNotFound {
		t.Fatalf("removed service diagnostic route returned %d, want 404", removedServiceDiagnostic.Code)
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
	if !paymentMethodPattern.MatchString("real_time_payment") || paymentMethodPattern.MatchString("QRIS / Xendit") {
		t.Fatal("payment method identifier validation is incorrect")
	}
	for _, status := range []string{"DOCUMENTED", "CERTIFIED", " documented "} {
		if !assignablePaymentMethodCapability(status) {
			t.Fatalf("assignable capability status %q was rejected", status)
		}
	}
	for _, status := range []string{"", "DISABLED", "UNKNOWN"} {
		if assignablePaymentMethodCapability(status) {
			t.Fatalf("non-assignable capability status %q was accepted", status)
		}
	}
}

func TestPaymentMethodEnvironmentQuery(t *testing.T) {
	optional := httptest.NewRequest(http.MethodGet, "/api/v1/payment-method-assignments", nil)
	if value, ok := optionalEnvironmentQuery(httptest.NewRecorder(), optional); !ok || value != "" {
		t.Fatalf("optional environment without query = %q, %v; want empty, true", value, ok)
	}

	filtered := httptest.NewRequest(http.MethodGet, "/api/v1/payment-method-assignments?environment=LIVE", nil)
	if value, ok := optionalEnvironmentQuery(httptest.NewRecorder(), filtered); !ok || value != "live" {
		t.Fatalf("filtered environment = %q, %v; want live, true", value, ok)
	}

	missingRecorder := httptest.NewRecorder()
	if value, ok := requireEnvironmentQuery(missingRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/payment-options", nil)); ok || value != "" || missingRecorder.Code != http.StatusBadRequest {
		t.Fatalf("missing required environment = %q, %v, status %d", value, ok, missingRecorder.Code)
	}
	if !strings.Contains(missingRecorder.Body.String(), "INVALID_ENVIRONMENT") {
		t.Fatalf("missing environment returned unexpected problem: %s", missingRecorder.Body.String())
	}

	invalidRecorder := httptest.NewRecorder()
	if value, ok := optionalEnvironmentQuery(invalidRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/payment-method-assignments?environment=staging", nil)); ok || value != "" || invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid optional environment = %q, %v, status %d", value, ok, invalidRecorder.Code)
	}
}

func TestCatalogSearchQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/providers?q=%20XeNdIt%20", nil)
	if search, ok := catalogSearchQuery(httptest.NewRecorder(), request); !ok || search != "XeNdIt" {
		t.Fatalf("catalog query = %q, %v; want XeNdIt, true", search, ok)
	}

	empty := httptest.NewRequest(http.MethodGet, "/api/v1/payment-methods", nil)
	if search, ok := catalogSearchQuery(httptest.NewRecorder(), empty); !ok || search != "" {
		t.Fatalf("empty catalog query = %q, %v; want empty, true", search, ok)
	}

	invalidRecorder := httptest.NewRecorder()
	invalid := httptest.NewRequest(http.MethodGet, "/api/v1/providers?q="+strings.Repeat("a", 129), nil)
	if search, ok := catalogSearchQuery(invalidRecorder, invalid); ok || search != "" || invalidRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("oversized catalog query = %q, %v, status %d", search, ok, invalidRecorder.Code)
	}
	if !strings.Contains(invalidRecorder.Body.String(), "INVALID_QUERY") {
		t.Fatalf("oversized catalog query returned unexpected problem: %s", invalidRecorder.Body.String())
	}
}

func TestPaymentMethodAssignmentBulkPayload(t *testing.T) {
	decode := func(body string) ([]paymentMethodAssignmentRequest, bool, bool, *httptest.ResponseRecorder) {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/payment-method-assignments", strings.NewReader(body))
		items, legacy, ok := decodePaymentMethodAssignmentRequests(response, request)
		return items, legacy, ok, response
	}

	items, legacy, ok, _ := decode(`{"assignments":[{"installation_id":"ins_a","payment_method_code":"qris","version":0},{"installation_id":"ins_a","payment_method_code":"va_bca","version":2}]}`)
	if !ok || legacy || len(items) != 2 || items[1].PaymentMethodCode != "va_bca" {
		t.Fatalf("valid bulk payload was not decoded: legacy=%v ok=%v items=%#v", legacy, ok, items)
	}
	items, legacy, ok, _ = decode(`{"installation_id":"ins_a","payment_method_code":"qris","version":0}`)
	if !ok || !legacy || len(items) != 1 || items[0].InstallationID != "ins_a" {
		t.Fatalf("legacy payload compatibility failed: legacy=%v ok=%v items=%#v", legacy, ok, items)
	}

	_, _, ok, response := decode(`{"assignments":[]}`)
	if ok || response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "INVALID_BATCH_SIZE") {
		t.Fatalf("empty batch was accepted: %d %s", response.Code, response.Body.String())
	}
	_, _, ok, response = decode(`{"assignments":[{"installation_id":"ins_a","payment_method_code":"qris","version":0}],"installation_id":"ins_a"}`)
	if ok || response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "VALIDATION_ERROR") {
		t.Fatalf("mixed bulk and legacy payload was accepted: %d %s", response.Code, response.Body.String())
	}

	tooMany := paymentMethodAssignmentsRequest{Assignments: &[]paymentMethodAssignmentRequest{}}
	for index := 0; index <= maxAssignmentBatch; index++ {
		*tooMany.Assignments = append(*tooMany.Assignments, paymentMethodAssignmentRequest{InstallationID: "ins_a", PaymentMethodCode: "qris"})
	}
	body, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	_, _, ok, response = decode(string(body))
	if ok || response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "INVALID_BATCH_SIZE") {
		t.Fatalf("oversized batch was accepted: %d %s", response.Code, response.Body.String())
	}
}

func TestCreatePaymentDoesNotRequireExecutionMode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	handler := New(config.Config{ServiceAPIKey: "test-service-key"}, nil, nil, nil, nil, nil, logger)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/payment-sessions", strings.NewReader(`{}`))
	request.Header.Set("Authorization", "Bearer test-service-key")
	request.Header.Set("X-Emisell-Merchant-ID", "merchant_test")
	request.Header.Set("Idempotency-Key", "checkout-contract-test")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "INVALID_EXECUTION_MODE") {
		t.Fatalf("create payment still required execution mode: %d %s", response.Code, response.Body.String())
	}
}

func TestProviderRegistryStatusAllowlist(t *testing.T) {
	for _, status := range []string{"DRAFT", "ACTIVE", "DISABLED"} {
		if !validProviderRegistryStatus(status) {
			t.Fatalf("valid provider registry status %q was rejected", status)
		}
	}
	for _, status := range []string{"", "PUBLISHED", "DELETED", "disabled"} {
		if validProviderRegistryStatus(status) {
			t.Fatalf("invalid provider registry status %q was accepted", status)
		}
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

func TestResponseHeaders(t *testing.T) {
	server := &Server{}
	handler := middleware.RequestID(server.responseHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))

	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, httptest.NewRequest(http.MethodGet, "/api/v1/engine/capabilities", nil))
	if accepted.Code != http.StatusNoContent {
		t.Fatalf("request was rejected: %d", accepted.Code)
	}
	if accepted.Header().Get("X-Request-ID") == "" || accepted.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response headers are incomplete: %#v", accepted.Header())
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
