package midtrans

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	maxRequestBytes  = 16 << 10
	maxResponseBytes = 1 << 20
)

type Client struct {
	sandboxURL       *url.URL
	liveURL          *url.URL
	sandboxSnapURL   *url.URL
	liveSnapURL      *url.URL
	httpClient       *http.Client
	executableSHA256 string
}

// SetExecutableSHA256 binds the connector capability handshake to the exact
// standalone Provider App executable. It is set once during process startup.
func (c *Client) SetExecutableSHA256(value string) {
	c.executableSHA256 = strings.ToLower(strings.TrimSpace(value))
}

type apiCredentials struct {
	serverKey string
	popID     string
}

func New(sandboxBaseURL, liveBaseURL string, timeout time.Duration) (*Client, error) {
	sandbox, err := parseBaseURL(sandboxBaseURL, "https://api.sandbox.midtrans.com")
	if err != nil {
		return nil, fmt.Errorf("invalid Midtrans sandbox base URL: %w", err)
	}
	live, err := parseBaseURL(liveBaseURL, "https://api.midtrans.com")
	if err != nil {
		return nil, fmt.Errorf("invalid Midtrans live base URL: %w", err)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		sandboxURL:     sandbox,
		liveURL:        live,
		sandboxSnapURL: midtransSnapBaseURL(sandbox, true),
		liveSnapURL:    midtransSnapBaseURL(live, false),
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Midtrans redirect is not allowed")
			},
		},
	}, nil
}

func midtransSnapBaseURL(core *url.URL, sandbox bool) *url.URL {
	result := *core
	switch strings.ToLower(core.Hostname()) {
	case "api.sandbox.midtrans.com":
		result.Host = "app.sandbox.midtrans.com"
	case "api.midtrans.com":
		result.Host = "app.midtrans.com"
	default:
		// Custom URLs (including test servers and egress proxies) serve both
		// Core API and Snap from the same origin.
		return &result
	}
	if sandbox {
		result.Host = "app.sandbox.midtrans.com"
	}
	result.Path = ""
	result.RawPath = ""
	result.RawQuery = ""
	result.Fragment = ""
	return &result
}

func parseBaseURL(value, fallback string) (*url.URL, error) {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("base URL must be HTTP(S) without credentials")
	}
	return parsed, nil
}

func (c *Client) Code() string { return "midtrans" }

func (c *Client) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	environment, err := midtransEnvironment(credentials.serverKey)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	probeID := "emisell-credential-check-" + strings.TrimPrefix(input.InstallationID, "ins_")
	if len(probeID) > 50 {
		probeID = probeID[:50]
	}
	var response map[string]any
	err = c.do(ctx, environment, http.MethodGet, "/v2/"+url.PathEscape(probeID)+"/status", credentials, nil, &response, false)
	if err != nil {
		var apiErr *connector.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			return connector.InstallationResult{}, err
		}
	}
	storedCredentials := map[string]string{"server_key": credentials.serverKey}
	if credentials.popID != "" {
		storedCredentials["pop_id"] = credentials.popID
	}
	return connector.InstallationResult{
		ConnectorID:       "midtrans:" + input.InstallationID,
		Environment:       environment,
		StoredCredentials: storedCredentials,
		WebhookReady:      strings.TrimSpace(input.PublicWebhookURL) != "",
	}, nil
}

func midtransEnvironment(serverKey string) (string, error) {
	switch {
	case strings.HasPrefix(serverKey, "SB-Mid-server-"):
		return "sandbox", nil
	case strings.HasPrefix(serverKey, "Mid-server-"):
		return "live", nil
	default:
		return "", fmt.Errorf("%w: Midtrans Server Key must identify a sandbox or production account", connector.ErrInvalidCredential)
	}
}

func (c *Client) DisableInstallation(context.Context, connector.InstallationInput) error { return nil }

func (c *Client) CreatePayment(ctx context.Context, input connector.PaymentInput) (connector.PaymentResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	amount, err := providerAmount(input.Amount, input.Currency)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	orderID, err := orderID(input)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if input.CheckoutMode == connector.CheckoutModeProviderHosted {
		return c.createSnapCheckout(ctx, input, credentials, amount, orderID)
	}
	payload := map[string]any{
		"transaction_details": map[string]any{"order_id": orderID, "gross_amount": amount},
	}
	if customer := customerDetails(input.Customer); len(customer) > 0 {
		payload["customer_details"] = customer
	}
	if items := itemDetails(input.Items, input.Currency, amount); len(items) > 0 {
		payload["item_details"] = items
	}
	switch input.PaymentMethodCode {
	case "qris":
		payload["payment_type"] = "qris"
		payload["qris"] = map[string]any{"acquirer": "gopay"}
	case "va_mandiri":
		payload["payment_type"] = "echannel"
		payload["echannel"] = map[string]any{"bill_info1": "Payment", "bill_info2": orderID}
	case "va_permata":
		payload["payment_type"] = "permata"
	case "va_bca", "va_bni", "va_bri", "va_cimb":
		payload["payment_type"] = "bank_transfer"
		payload["bank_transfer"] = map[string]any{"bank": strings.TrimPrefix(input.PaymentMethodCode, "va_")}
	case "ewallet_gopay":
		payload["payment_type"] = "gopay"
		options := map[string]any{}
		if isHTTPSURL(input.ReturnURL) {
			options["enable_callback"] = true
			options["callback_url"] = strings.TrimSpace(input.ReturnURL)
		}
		if len(options) > 0 {
			payload["gopay"] = options
		}
	case "ewallet_shopeepay":
		payload["payment_type"] = "shopeepay"
		if isHTTPSURL(input.ReturnURL) {
			payload["shopeepay"] = map[string]any{"callback_url": strings.TrimSpace(input.ReturnURL)}
		}
	default:
		return connector.PaymentResult{}, connector.ErrNotSupported
	}
	var response map[string]any
	headers := make(http.Header)
	if isHTTPSURL(input.PublicWebhookURL) {
		headers.Set("X-Override-Notification", strings.TrimSpace(input.PublicWebhookURL))
	}
	if err = c.doWithHeaders(ctx, input.Environment, http.MethodPost, "/v2/charge", credentials, headers, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	result, err := paymentResult(response, orderID, input.PaymentMethodCode)
	if err != nil {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: err}
	}
	return result, nil
}

func (c *Client) createSnapCheckout(ctx context.Context, input connector.PaymentInput, credentials apiCredentials, amount int64, orderID string) (connector.PaymentResult, error) {
	enabledPayments, err := c.hostedPaymentTypes(input.AllowedPaymentMethods)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	payload := map[string]any{
		"transaction_details": map[string]any{"order_id": orderID, "gross_amount": amount},
		"credit_card":         map[string]any{"secure": true},
		"enabled_payments":    enabledPayments,
	}
	if customer := customerDetails(input.Customer); len(customer) > 0 {
		payload["customer_details"] = customer
	}
	if items := itemDetails(input.Items, input.Currency, amount); len(items) > 0 {
		payload["item_details"] = items
	}
	if returnURL := strings.TrimSpace(input.ReturnURL); returnURL != "" {
		if !isHTTPSURL(returnURL) {
			return connector.PaymentResult{}, errors.New("return_url must be HTTPS for Midtrans hosted checkout")
		}
		payload["callbacks"] = map[string]any{"finish": returnURL, "error": returnURL}
	}
	headers := make(http.Header)
	if isHTTPSURL(input.PublicWebhookURL) {
		headers.Set("X-Override-Notification", strings.TrimSpace(input.PublicWebhookURL))
	}
	var response map[string]any
	if err := c.doSnap(ctx, input.Environment, http.MethodPost, "/snap/v1/transactions", credentials, headers, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	redirectURL := firstString(response, "redirect_url")
	if !isHTTPSURL(redirectURL) {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: errors.New("Midtrans Snap response did not contain a valid redirect_url")}
	}
	nextAction, _ := json.Marshal(map[string]any{"type": "redirect", "redirect_url": redirectURL})
	return connector.PaymentResult{
		ID: orderID, Status: "REQUIRES_ACTION", ConnectorTransactionID: orderID,
		NextAction: nextAction,
	}, nil
}

func (c *Client) hostedPaymentTypes(methods []connector.PaymentMethodMapping) ([]string, error) {
	if len(methods) == 0 {
		return nil, errors.New("at least one active payment method is required for Midtrans hosted checkout")
	}
	types := make([]string, 0, len(methods))
	seen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		if err := c.ValidatePaymentMethod(method); err != nil {
			return nil, err
		}
		mapping := supportedMethods[strings.ToLower(strings.TrimSpace(method.PaymentMethodCode))]
		channelCode := strings.ToLower(strings.TrimSpace(method.ProviderChannelCode))
		if channelCode == "" {
			channelCode = mapping.channelCode
		}
		if _, exists := seen[channelCode]; exists {
			continue
		}
		seen[channelCode] = struct{}{}
		types = append(types, channelCode)
	}
	return types, nil
}

func (c *Client) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if strings.TrimSpace(input.PaymentID) == "" {
		return connector.PaymentResult{}, errors.New("Midtrans payment_id is required")
	}
	var response map[string]any
	if err = c.do(ctx, input.Environment, http.MethodGet, "/v2/"+url.PathEscape(input.PaymentID)+"/status", credentials, nil, &response, false); err != nil {
		return connector.PaymentResult{}, err
	}
	return paymentResult(response, input.PaymentID, firstString(response, "payment_type"))
}

func (c *Client) CapturePayment(context.Context, connector.CaptureInput) (connector.PaymentResult, error) {
	return connector.PaymentResult{}, connector.ErrNotSupported
}

func (c *Client) CancelPayment(ctx context.Context, input connector.PaymentLookup, _, _ string) (connector.PaymentResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if strings.TrimSpace(input.PaymentID) == "" {
		return connector.PaymentResult{}, errors.New("Midtrans payment_id is required")
	}
	var response map[string]any
	path := "/v2/" + url.PathEscape(input.PaymentID) + "/cancel"
	if err = c.do(ctx, input.Environment, http.MethodPost, path, credentials, map[string]any{}, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	return paymentResult(response, input.PaymentID, firstString(response, "payment_type"))
}

func (c *Client) SimulatePayment(context.Context, connector.PaymentLookup, int64, string) error {
	return connector.ErrNotSupported
}

func (c *Client) CreateRefund(ctx context.Context, input connector.RefundInput) (connector.RefundResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.RefundResult{}, err
	}
	if strings.TrimSpace(input.PaymentID) == "" {
		return connector.RefundResult{}, errors.New("Midtrans payment_id is required")
	}
	amount, err := providerAmount(input.Amount, input.Currency)
	if err != nil {
		return connector.RefundResult{}, err
	}
	refundKey := stableRefundKey(input.IdempotencyKey)
	payload := map[string]any{"refund_key": refundKey, "amount": amount}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		if len(reason) > 255 {
			reason = reason[:255]
		}
		payload["reason"] = reason
	}
	var response map[string]any
	path := "/v2/" + url.PathEscape(input.PaymentID) + "/refund"
	if err = c.do(ctx, input.Environment, http.MethodPost, path, credentials, payload, &response, true); err != nil {
		return connector.RefundResult{}, err
	}
	return connector.RefundResult{
		ID: input.PaymentID + "|" + refundKey,
		// The regular Refund API acknowledges the request before the acquiring
		// bank/payment provider confirms it. Midtrans recommends waiting for the
		// later notification carrying bank_confirmed_at.
		Status: midtransRefundStatus(firstString(response, "transaction_status", "refund_status", "status_code"), firstString(response, "bank_confirmed_at")),
	}, nil
}

func (c *Client) GetRefund(ctx context.Context, input connector.RefundLookup) (connector.RefundResult, error) {
	parts := strings.SplitN(input.RefundID, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return connector.RefundResult{}, errors.New("invalid Midtrans refund_id")
	}
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.RefundResult{}, err
	}
	var response map[string]any
	if err = c.do(ctx, input.Environment, http.MethodGet, "/v2/"+url.PathEscape(parts[0])+"/status", credentials, nil, &response, false); err != nil {
		return connector.RefundResult{}, err
	}
	refunds, _ := response["refunds"].([]any)
	for _, raw := range refunds {
		item, _ := raw.(map[string]any)
		if firstString(item, "refund_key") == parts[1] {
			status := firstString(item, "refund_status", "status")
			if status == "" {
				status = firstString(response, "transaction_status")
			}
			return connector.RefundResult{ID: input.RefundID, Status: midtransRefundStatus(status, firstString(item, "bank_confirmed_at"))}, nil
		}
	}
	return connector.RefundResult{}, &connector.APIError{Provider: "midtrans", Status: http.StatusNotFound, Code: "REFUND_NOT_FOUND", Message: "refund was not found in Midtrans transaction status"}
}

func (c *Client) HandleWebhook(_ context.Context, input connector.WebhookInput) (connector.WebhookEvent, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.WebhookEvent{}, err
	}
	var payload map[string]any
	if err = json.Unmarshal(input.Body, &payload); err != nil {
		return connector.WebhookEvent{}, errors.New("invalid Midtrans webhook JSON")
	}
	orderID := firstString(payload, "order_id")
	statusCode := firstString(payload, "status_code")
	grossAmount := firstString(payload, "gross_amount")
	provided := strings.ToLower(strings.TrimSpace(firstString(payload, "signature_key")))
	if orderID == "" || statusCode == "" || grossAmount == "" || provided == "" {
		return connector.WebhookEvent{}, &connector.APIError{Provider: "midtrans", Status: http.StatusUnauthorized, Code: "INVALID_WEBHOOK_SIGNATURE"}
	}
	digest := sha512.Sum512([]byte(orderID + statusCode + grossAmount + credentials.serverKey))
	expected := hex.EncodeToString(digest[:])
	if !hmac.Equal([]byte(provided), []byte(expected)) {
		return connector.WebhookEvent{}, &connector.APIError{Provider: "midtrans", Status: http.StatusUnauthorized, Code: "INVALID_WEBHOOK_SIGNATURE"}
	}
	transactionStatus := firstString(payload, "transaction_status")
	refundKey, refundProviderStatus, bankConfirmedAt := midtransRefundDetails(payload)
	eventType := "payment.updated"
	refundID := ""
	if refundKey != "" || transactionStatus == "refund" || transactionStatus == "partial_refund" {
		eventType = "refund.updated"
		if refundKey != "" {
			refundID = orderID + "|" + refundKey
		}
	}
	eventID := firstString(payload, "transaction_id") + ":" + transactionStatus + ":" + refundKey + ":" + bankConfirmedAt
	if strings.Trim(eventID, ":") == "" {
		digest := sha256.Sum256(input.Body)
		eventID = hex.EncodeToString(digest[:])
	}
	status := paymentStatus(transactionStatus, firstString(payload, "fraud_status"))
	if eventType == "refund.updated" {
		status = midtransRefundStatus(refundProviderStatus, bankConfirmedAt)
	}
	return connector.WebhookEvent{
		ID: eventID, Type: eventType, PaymentID: orderID, RefundID: refundID,
		Status: status,
	}, nil
}

func midtransRefundDetails(payload map[string]any) (refundKey, status, bankConfirmedAt string) {
	refundKey = firstString(payload, "refund_key")
	status = firstString(payload, "refund_status", "transaction_status")
	bankConfirmedAt = firstString(payload, "bank_confirmed_at")
	refunds, _ := payload["refunds"].([]any)
	for index := len(refunds) - 1; index >= 0; index-- {
		item, _ := refunds[index].(map[string]any)
		candidateKey := firstString(item, "refund_key")
		if refundKey != "" && candidateKey != refundKey {
			continue
		}
		if refundKey == "" {
			refundKey = candidateKey
		}
		if itemStatus := firstString(item, "refund_status", "status"); itemStatus != "" {
			status = itemStatus
		}
		if confirmed := firstString(item, "bank_confirmed_at"); confirmed != "" {
			bankConfirmedAt = confirmed
		}
		break
	}
	return refundKey, status, bankConfirmedAt
}

func (c *Client) do(ctx context.Context, environment, method, path string, credentials apiCredentials, payload, target any, mutation bool) error {
	return c.doWithHeaders(ctx, environment, method, path, credentials, nil, payload, target, mutation)
}

func (c *Client) doWithHeaders(ctx context.Context, environment, method, path string, credentials apiCredentials, headers http.Header, payload, target any, mutation bool) error {
	baseURL, err := c.baseURL(environment)
	if err != nil {
		return err
	}
	return c.doAtBaseURL(ctx, baseURL, method, path, credentials, headers, payload, target, mutation)
}

func (c *Client) doSnap(ctx context.Context, environment, method, path string, credentials apiCredentials, headers http.Header, payload, target any, mutation bool) error {
	var baseURL *url.URL
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "sandbox":
		baseURL = c.sandboxSnapURL
	case "live":
		baseURL = c.liveSnapURL
	default:
		return errors.New("Midtrans environment must be sandbox or live")
	}
	return c.doAtBaseURL(ctx, baseURL, method, path, credentials, headers, payload, target, mutation)
}

func (c *Client) doAtBaseURL(ctx context.Context, baseURL *url.URL, method, path string, credentials apiCredentials, headers http.Header, payload, target any, mutation bool) error {
	endpoint := baseURL.ResolveReference(&url.URL{Path: path})
	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return encodeErr
		}
		if len(encoded) > maxRequestBytes {
			return errors.New("Midtrans request exceeds 16 KiB")
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	request.SetBasicAuth(credentials.serverKey, "")
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	if credentials.popID != "" {
		request.Header.Set("X-POP-ID", credentials.popID)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Emisell-Connector-Runner/1.0")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	if len(responseBytes) > maxResponseBytes {
		if mutation {
			return &connector.UnknownOutcomeError{Cause: errors.New("Midtrans response exceeds 1 MiB")}
		}
		return errors.New("Midtrans response exceeds 1 MiB")
	}
	var decoded map[string]any
	if len(responseBytes) > 0 {
		_ = json.Unmarshal(responseBytes, &decoded)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := midtransAPIError(response.StatusCode, decoded)
		if mutation && (response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError) {
			return &connector.UnknownOutcomeError{Cause: apiErr}
		}
		return apiErr
	}
	// Some Midtrans Core API failures use HTTP 200 while the JSON status_code
	// carries the actual 4xx/5xx outcome. Never turn those responses into a
	// synthetic PENDING payment merely because the transport was successful.
	if code := firstString(decoded, "status_code"); code != "" {
		providerStatus, parseErr := strconv.Atoi(code)
		if parseErr == nil && providerStatus >= 400 {
			apiErr := midtransAPIError(providerStatus, decoded)
			if mutation && (providerStatus == http.StatusRequestTimeout || providerStatus >= http.StatusInternalServerError) {
				return &connector.UnknownOutcomeError{Cause: apiErr}
			}
			return apiErr
		}
	}
	if target != nil && len(responseBytes) > 0 {
		if err = json.Unmarshal(responseBytes, target); err != nil {
			if mutation {
				return &connector.UnknownOutcomeError{Cause: err}
			}
			return err
		}
	}
	return nil
}

func (c *Client) baseURL(environment string) (*url.URL, error) {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "sandbox":
		return c.sandboxURL, nil
	case "live":
		return c.liveURL, nil
	default:
		return nil, errors.New("Midtrans environment must be sandbox or live")
	}
}

func parseCredentials(input map[string]string) (apiCredentials, error) {
	credentials := apiCredentials{
		serverKey: strings.TrimSpace(input["server_key"]),
		popID:     strings.TrimSpace(input["pop_id"]),
	}
	if credentials.serverKey == "" {
		return apiCredentials{}, errors.New("Midtrans server_key is required")
	}
	if len(credentials.popID) > 128 || strings.ContainsAny(credentials.popID, "\r\n") {
		return apiCredentials{}, errors.New("Midtrans pop_id is invalid")
	}
	return credentials, nil
}

func midtransAPIError(status int, response map[string]any) *connector.APIError {
	message := errorMessage(response)
	code := firstString(response, "status_code")
	if code == "" {
		code = "MIDTRANS_ERROR"
	}
	switch strings.ToLower(strings.TrimSpace(message)) {
	case "merchant pop id is not found":
		code = "MIDTRANS_POP_ID_REQUIRED"
		message = "Midtrans requires a valid PoP ID for this merchant and payment channel"
	case "payment channel is not activated.", "payment channel is not activated":
		code = "MIDTRANS_PAYMENT_CHANNEL_NOT_ACTIVE"
		message = "The selected Midtrans payment channel is not active for this merchant"
	}
	return &connector.APIError{Provider: "midtrans", Status: status, Code: code, Message: message}
}

func providerAmount(amount int64, currency string) (int64, error) {
	if amount <= 0 {
		return 0, errors.New("payment amount must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return 0, errors.New("Midtrans Core API connector currently supports IDR only")
	}
	return amount, nil
}

func orderID(input connector.PaymentInput) (string, error) {
	value := strings.TrimSpace(input.LocalPaymentID)
	if value == "" {
		value = strings.TrimSpace(input.MerchantReference)
	}
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "", errors.New("local_payment_id or merchant_reference is required for Midtrans")
	}
	if len(value) > 50 {
		value = value[:50]
	}
	return value, nil
}

func customerDetails(customer connector.Customer) map[string]any {
	result := map[string]any{}
	if name := strings.TrimSpace(customer.Name); name != "" {
		fields := strings.Fields(name)
		result["first_name"] = fields[0]
		if len(fields) > 1 {
			result["last_name"] = strings.Join(fields[1:], " ")
		}
	}
	if email := strings.TrimSpace(customer.Email); email != "" {
		result["email"] = email
	}
	if phone := strings.TrimSpace(customer.Phone); phone != "" {
		result["phone"] = phone
	}
	return result
}

func itemDetails(items []connector.Item, currency string, grossAmount int64) []map[string]any {
	if !strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return nil
	}
	result := make([]map[string]any, 0, len(items))
	var total int64
	for index, item := range items {
		price, err := providerAmount(item.NetUnitAmount, currency)
		if err != nil {
			return nil
		}
		quantity := item.Quantity
		if quantity < 1 {
			quantity = 1
		}
		id := strings.TrimSpace(item.ReferenceID)
		if id == "" {
			id = "item-" + strconv.Itoa(index+1)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "Emisell item"
		}
		result = append(result, map[string]any{"id": id, "price": price, "quantity": quantity, "name": name})
		total += price * int64(quantity)
	}
	if total != grossAmount {
		return nil
	}
	return result
}

func paymentResult(response map[string]any, fallbackID, method string) (connector.PaymentResult, error) {
	id := firstString(response, "order_id")
	if id == "" {
		id = fallbackID
	}
	if id == "" {
		return connector.PaymentResult{}, errors.New("Midtrans payment response did not contain an order ID")
	}
	result := connector.PaymentResult{
		ID:                     id,
		Status:                 paymentStatus(firstString(response, "transaction_status"), firstString(response, "fraud_status")),
		ConnectorTransactionID: firstString(response, "transaction_id"),
	}
	if result.Status == "" {
		result.Status = "PENDING"
	}
	if action := nextAction(response, method); action != nil {
		result.NextAction, _ = json.Marshal(action)
	}
	return result, nil
}

func paymentStatus(status, fraudStatus string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "capture":
		switch strings.ToLower(strings.TrimSpace(fraudStatus)) {
		case "challenge":
			return "PENDING"
		case "deny":
			return "FAILED"
		default:
			return "SUCCEEDED"
		}
	case "settlement", "refund", "partial_refund":
		if strings.EqualFold(strings.TrimSpace(fraudStatus), "deny") {
			return "FAILED"
		}
		return "SUCCEEDED"
	case "pending", "authorize":
		return "PENDING"
	case "deny", "failure":
		return "FAILED"
	case "cancel":
		return "CANCELLED"
	case "expire":
		return "EXPIRED"
	default:
		return strings.ToUpper(strings.TrimSpace(status))
	}
}

func midtransRefundStatus(status, bankConfirmedAt string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "deny", "failure", "failed":
		return "FAILED"
	}
	if strings.TrimSpace(bankConfirmedAt) != "" {
		return "SUCCEEDED"
	}
	return "PENDING"
}

func nextAction(response map[string]any, method string) map[string]any {
	if vaNumbers, ok := response["va_numbers"].([]any); ok && len(vaNumbers) > 0 {
		va, _ := vaNumbers[0].(map[string]any)
		return map[string]any{
			"type": "virtual_account_information", "bank": firstString(va, "bank"),
			"va_number": firstString(va, "va_number"), "display_text": firstString(va, "va_number"),
		}
	}
	if value := firstString(response, "permata_va_number"); value != "" {
		return map[string]any{"type": "virtual_account_information", "bank": "permata", "va_number": value, "display_text": value}
	}
	if billKey := firstString(response, "bill_key"); billKey != "" {
		return map[string]any{
			"type": "virtual_account_information", "bank": "mandiri", "bill_key": billKey,
			"biller_code": firstString(response, "biller_code"), "display_text": billKey,
		}
	}
	actions, _ := response["actions"].([]any)
	var redirectURL string
	for _, raw := range actions {
		action, _ := raw.(map[string]any)
		name := strings.ToLower(firstString(action, "name"))
		value := firstString(action, "url")
		if value == "" {
			continue
		}
		if strings.Contains(name, "generate-qr-code") {
			return map[string]any{"type": "qr_code_information", "qr_code_url": value, "actions": actions}
		}
		if strings.Contains(name, "deeplink") || strings.Contains(name, "redirect") {
			redirectURL = value
		}
	}
	if redirectURL != "" {
		return map[string]any{"type": "redirect", "redirect_url": redirectURL, "actions": actions}
	}
	if len(actions) > 0 {
		return map[string]any{"type": "provider_actions", "actions": actions}
	}
	return nil
}

func stableRefundKey(value string) string {
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.TrimSpace(value))
	value = strings.Trim(value, "-")
	if value == "" || len(value) > 40 {
		digest := sha256.Sum256([]byte(value))
		value = "emisell-" + hex.EncodeToString(digest[:12])
	}
	return value
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func firstString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := input[key].(type) {
		case string:
			if strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		case json.Number:
			return value.String()
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64)
		}
	}
	return ""
}

func errorMessage(response map[string]any) string {
	if value := firstString(response, "status_message", "message"); value != "" {
		return value
	}
	if messages, ok := response["error_messages"].([]any); ok {
		result := make([]string, 0, len(messages))
		for _, message := range messages {
			if value, ok := message.(string); ok && strings.TrimSpace(value) != "" {
				result = append(result, strings.TrimSpace(value))
			}
		}
		return strings.Join(result, "; ")
	}
	return "Midtrans rejected the request"
}
