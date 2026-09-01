package doku

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	maxRequestBytes            = 64 << 10
	maxResponseBytes           = 1 << 20
	webhookRequestTargetHeader = "X-Emisell-Webhook-Request-Target"
	checkoutPaymentPath        = "/checkout/v1/payment"
	checkStatusPathPrefix      = "/orders/v1/status/"
)

type Client struct {
	sandboxURL       *url.URL
	liveURL          *url.URL
	httpClient       *http.Client
	executableSHA256 string
	now              func() time.Time
}

type apiCredentials struct {
	clientID  string
	secretKey string
}

func New(sandboxBaseURL, liveBaseURL string, timeout time.Duration) (*Client, error) {
	sandbox, err := parseBaseURL(sandboxBaseURL, "https://api-sandbox.doku.com")
	if err != nil {
		return nil, fmt.Errorf("invalid DOKU sandbox base URL: %w", err)
	}
	live, err := parseBaseURL(liveBaseURL, "https://api.doku.com")
	if err != nil {
		return nil, fmt.Errorf("invalid DOKU live base URL: %w", err)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		sandboxURL: sandbox,
		liveURL:    live,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("DOKU redirect is not allowed")
			},
		},
		now: time.Now,
	}, nil
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

func (c *Client) SetExecutableSHA256(value string) {
	c.executableSHA256 = strings.ToLower(strings.TrimSpace(value))
}

func (c *Client) Code() string { return "doku" }

func (c *Client) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	environment := strings.ToLower(strings.TrimSpace(input.Environment))
	if _, err = c.baseURL(environment); err != nil {
		return connector.InstallationResult{}, err
	}
	probeSeed := credentials.clientID + ":" + input.InstallationID
	probeInvoice := "EMSVERIFY" + digestHex(probeSeed, 20)
	requestID := stableRequestID("verify:" + probeSeed + ":" + c.timestamp())
	var response map[string]any
	err = c.do(ctx, environment, http.MethodGet, checkStatusPathPrefix+url.PathEscape(probeInvoice), credentials, requestID, nil, &response, false)
	if err != nil {
		var apiErr *connector.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
			return connector.InstallationResult{}, fmt.Errorf("%w: %v", connector.ErrInvalidCredential, err)
		}
	}
	return connector.InstallationResult{
		ConnectorID:  "doku:" + input.InstallationID,
		Environment:  environment,
		WebhookReady: isHTTPSURL(input.PublicWebhookURL),
		StoredCredentials: map[string]string{
			"client_id":  credentials.clientID,
			"secret_key": credentials.secretKey,
		},
	}, nil
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
	if !isHTTPSURL(input.ReturnURL) {
		return connector.PaymentResult{}, errors.New("return_url must be a public HTTPS URL for DOKU Checkout")
	}
	if !isHTTPSURL(input.PublicWebhookURL) {
		return connector.PaymentResult{}, errors.New("the installation webhook URL must be public HTTPS for DOKU Checkout")
	}
	invoiceNumber := invoiceNumber(input)
	order := map[string]any{
		"amount":              amount,
		"invoice_number":      invoiceNumber,
		"currency":            "IDR",
		"callback_url":        strings.TrimSpace(input.ReturnURL),
		"callback_url_cancel": strings.TrimSpace(input.ReturnURL),
		"callback_url_result": strings.TrimSpace(input.ReturnURL),
		"auto_redirect":       true,
	}
	if items := lineItems(input.Items, input.Currency, amount); len(items) > 0 {
		order["line_items"] = items
	}
	payment := map[string]any{"type": "SALE"}
	if input.CheckoutMode == connector.CheckoutModeProviderHosted {
		channels, channelErr := c.hostedPaymentTypes(input.AllowedPaymentMethods)
		if channelErr != nil {
			return connector.PaymentResult{}, channelErr
		}
		payment["payment_method_types"] = channels
	} else {
		channel, channelErr := dokuChannel(input.PaymentMethodCode, input.ChannelCode)
		if channelErr != nil {
			return connector.PaymentResult{}, channelErr
		}
		payment["payment_method_types"] = []string{channel}
	}
	if dueDate, dueErr := paymentDueDate(input.ExpiresAt, c.now()); dueErr != nil {
		return connector.PaymentResult{}, dueErr
	} else if dueDate > 0 {
		payment["payment_due_date"] = dueDate
	}
	payload := map[string]any{
		"order":           order,
		"payment":         payment,
		"additional_info": map[string]any{"override_notification_url": strings.TrimSpace(input.PublicWebhookURL)},
	}
	if customer := customerDetails(input.Customer); len(customer) > 0 {
		payload["customer"] = customer
	}
	requestID := stableRequestID("create:" + input.IdempotencyKey + ":" + invoiceNumber)
	var response map[string]any
	if err = c.do(ctx, input.Environment, http.MethodPost, checkoutPaymentPath, credentials, requestID, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	providerResponse := nestedMap(response, "response")
	responseOrder := nestedMap(providerResponse, "order")
	responsePayment := nestedMap(providerResponse, "payment")
	if value := firstString(responseOrder, "invoice_number"); value != "" {
		invoiceNumber = value
	}
	redirectURL := firstString(responsePayment, "url")
	if !isHTTPSURL(redirectURL) {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: errors.New("DOKU response did not contain a valid payment.url")}
	}
	nextAction, _ := json.Marshal(map[string]any{"type": "redirect", "redirect_url": redirectURL})
	connectorTransactionID := firstString(responseOrder, "session_id")
	if connectorTransactionID == "" {
		connectorTransactionID = firstString(responsePayment, "token_id")
	}
	return connector.PaymentResult{
		ID:                     invoiceNumber,
		Status:                 "REQUIRES_ACTION",
		ConnectorTransactionID: connectorTransactionID,
		NextAction:             nextAction,
	}, nil
}

func (c *Client) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	invoice := strings.TrimSpace(input.PaymentID)
	if invoice == "" {
		return connector.PaymentResult{}, errors.New("DOKU payment_id is required")
	}
	requestID := stableRequestID("status:" + invoice + ":" + c.timestamp())
	var response map[string]any
	if err = c.do(ctx, input.Environment, http.MethodGet, checkStatusPathPrefix+url.PathEscape(invoice), credentials, requestID, nil, &response, false); err != nil {
		return connector.PaymentResult{}, err
	}
	order := nestedMap(response, "order")
	transaction := nestedMap(response, "transaction")
	return connector.PaymentResult{
		ID:                     invoice,
		Status:                 paymentStatus(firstString(transaction, "status"), firstString(order, "status")),
		ConnectorTransactionID: firstString(transaction, "original_request_id"),
	}, nil
}

func (c *Client) CapturePayment(context.Context, connector.CaptureInput) (connector.PaymentResult, error) {
	return connector.PaymentResult{}, connector.ErrNotSupported
}

func (c *Client) CancelPayment(context.Context, connector.PaymentLookup, string, string) (connector.PaymentResult, error) {
	return connector.PaymentResult{}, connector.ErrNotSupported
}

func (c *Client) SimulatePayment(context.Context, connector.PaymentLookup, int64, string) error {
	return connector.ErrNotSupported
}

func (c *Client) CreateRefund(context.Context, connector.RefundInput) (connector.RefundResult, error) {
	return connector.RefundResult{}, connector.ErrNotSupported
}

func (c *Client) GetRefund(context.Context, connector.RefundLookup) (connector.RefundResult, error) {
	return connector.RefundResult{}, connector.ErrNotSupported
}

func (c *Client) HandleWebhook(_ context.Context, input connector.WebhookInput) (connector.WebhookEvent, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.WebhookEvent{}, err
	}
	clientID := strings.TrimSpace(input.Headers.Get("Client-Id"))
	requestID := strings.TrimSpace(input.Headers.Get("Request-Id"))
	timestamp := strings.TrimSpace(input.Headers.Get("Request-Timestamp"))
	providedSignature := strings.TrimSpace(input.Headers.Get("Signature"))
	requestTarget := strings.TrimSpace(input.Headers.Get(webhookRequestTargetHeader))
	if clientID == "" || clientID != credentials.clientID || requestID == "" || timestamp == "" || providedSignature == "" || !validRequestTarget(requestTarget) {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	expected := requestSignature(credentials, requestID, timestamp, requestTarget, input.Body)
	if !hmac.Equal([]byte(providedSignature), []byte(expected)) {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	var payload map[string]any
	if err = json.Unmarshal(input.Body, &payload); err != nil {
		return connector.WebhookEvent{}, errors.New("invalid DOKU notification JSON")
	}
	order := nestedMap(payload, "order")
	transaction := nestedMap(payload, "transaction")
	invoice := firstString(order, "invoice_number")
	if invoice == "" {
		return connector.WebhookEvent{}, errors.New("DOKU notification is missing order.invoice_number")
	}
	return connector.WebhookEvent{
		ID:        requestID,
		Type:      "payment.updated",
		PaymentID: invoice,
		Status:    paymentStatus(firstString(transaction, "status"), firstString(order, "status")),
	}, nil
}

func (c *Client) do(ctx context.Context, environment, method, requestPath string, credentials apiCredentials, requestID string, payload, target any, mutation bool) error {
	baseURL, err := c.baseURL(environment)
	if err != nil {
		return err
	}
	var bodyBytes []byte
	if payload != nil {
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		if len(bodyBytes) > maxRequestBytes {
			return errors.New("DOKU request exceeds 64 KiB")
		}
	}
	endpoint := *baseURL
	endpoint.Path = path.Join(baseURL.Path, requestPath)
	var body io.Reader
	if bodyBytes != nil {
		body = bytes.NewReader(bodyBytes)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	timestamp := c.timestamp()
	request.Header.Set("Client-Id", credentials.clientID)
	request.Header.Set("Request-Id", requestID)
	request.Header.Set("Request-Timestamp", timestamp)
	request.Header.Set("Signature", requestSignature(credentials, requestID, timestamp, requestPath, bodyBytes))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Emisell-Connector-Runner/1.0")
	if bodyBytes != nil {
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
		err = errors.New("DOKU response exceeds 1 MiB")
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	decoded := map[string]any{}
	if len(responseBytes) > 0 {
		_ = json.Unmarshal(responseBytes, &decoded)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		if signature := strings.TrimSpace(response.Header.Get("Signature")); signature != "" {
			if err = validateResponseSignature(response.Header, credentials, requestID, requestPath, responseBytes); err != nil {
				if mutation {
					return &connector.UnknownOutcomeError{Cause: err}
				}
				return err
			}
		}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := dokuAPIError(response.StatusCode, decoded)
		if mutation && (response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError) {
			return &connector.UnknownOutcomeError{Cause: apiErr}
		}
		return apiErr
	}
	if target != nil {
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
		return nil, errors.New("DOKU environment must be sandbox or live")
	}
}

func (c *Client) timestamp() string {
	return c.now().UTC().Format("2006-01-02T15:04:05Z")
}

func parseCredentials(input map[string]string) (apiCredentials, error) {
	credentials := apiCredentials{
		clientID:  strings.TrimSpace(input["client_id"]),
		secretKey: strings.TrimSpace(input["secret_key"]),
	}
	if credentials.clientID == "" || len(credentials.clientID) > 128 || strings.ContainsAny(credentials.clientID, "\r\n") {
		return apiCredentials{}, errors.New("DOKU client_id is required and must be valid")
	}
	if credentials.secretKey == "" || len(credentials.secretKey) > 512 || strings.ContainsAny(credentials.secretKey, "\r\n") {
		return apiCredentials{}, errors.New("DOKU secret_key is required and must be valid")
	}
	return credentials, nil
}

func requestSignature(credentials apiCredentials, requestID, timestamp, requestTarget string, body []byte) string {
	components := []string{
		"Client-Id:" + credentials.clientID,
		"Request-Id:" + requestID,
		"Request-Timestamp:" + timestamp,
		"Request-Target:" + requestTarget,
	}
	if body != nil {
		components = append(components, "Digest:"+bodyDigest(body))
	}
	return hmacSignature(credentials.secretKey, strings.Join(components, "\n"))
}

func validateResponseSignature(headers http.Header, credentials apiCredentials, requestID, requestTarget string, body []byte) error {
	clientID := strings.TrimSpace(headers.Get("Client-Id"))
	headerRequestID := strings.TrimSpace(headers.Get("Request-Id"))
	timestamp := strings.TrimSpace(headers.Get("Response-Timestamp"))
	provided := strings.TrimSpace(headers.Get("Signature"))
	if clientID != credentials.clientID || headerRequestID != requestID || timestamp == "" || provided == "" {
		return errors.New("DOKU response signature headers are incomplete")
	}
	components := strings.Join([]string{
		"Client-Id:" + clientID,
		"Request-Id:" + headerRequestID,
		"Response-Timestamp:" + timestamp,
		"Request-Target:" + requestTarget,
		"Digest:" + bodyDigest(body),
	}, "\n")
	expected := hmacSignature(credentials.secretKey, components)
	if !hmac.Equal([]byte(provided), []byte(expected)) {
		return errors.New("DOKU response signature is invalid")
	}
	return nil
}

func bodyDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return base64.StdEncoding.EncodeToString(digest[:])
}

func hmacSignature(secret, value string) string {
	digest := hmac.New(sha256.New, []byte(secret))
	_, _ = digest.Write([]byte(value))
	return "HMACSHA256=" + base64.StdEncoding.EncodeToString(digest.Sum(nil))
}

func stableRequestID(seed string) string {
	return "emisell-" + digestHex(seed, 32)
}

func digestHex(value string, length int) string {
	digest := sha256.Sum256([]byte(value))
	encoded := hex.EncodeToString(digest[:])
	if length > len(encoded) {
		length = len(encoded)
	}
	return encoded[:length]
}

func providerAmount(amount int64, currency string) (int64, error) {
	if amount <= 0 {
		return 0, errors.New("payment amount must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return 0, errors.New("DOKU Checkout connector supports IDR only")
	}
	if amount > 999_999_999_999 {
		return 0, errors.New("DOKU payment amount exceeds the 12-digit provider limit")
	}
	return amount, nil
}

func invoiceNumber(input connector.PaymentInput) string {
	seed := strings.TrimSpace(input.LocalPaymentID)
	if seed == "" {
		seed = strings.TrimSpace(input.MerchantReference)
	}
	if seed == "" {
		seed = strings.TrimSpace(input.IdempotencyKey)
	}
	return "EMS" + strings.ToUpper(digestHex(seed, 24))
}

func customerDetails(customer connector.Customer) map[string]any {
	result := map[string]any{}
	if name := strings.TrimSpace(customer.Name); name != "" {
		if len(name) > 255 {
			name = name[:255]
		}
		result["name"] = name
	}
	if email := strings.TrimSpace(customer.Email); email != "" {
		if len(email) > 128 {
			email = email[:128]
		}
		result["email"] = email
	}
	if phone := strings.TrimSpace(customer.Phone); phone != "" {
		if len(phone) > 16 {
			phone = phone[:16]
		}
		result["phone"] = phone
	}
	return result
}

func lineItems(items []connector.Item, currency string, expected int64) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	var total int64
	for index, item := range items {
		price, err := providerItemAmount(item.NetUnitAmount, currency)
		if err != nil {
			return nil
		}
		quantity := item.Quantity
		if quantity < 1 {
			quantity = 1
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "Emisell item"
		}
		if len(name) > 255 {
			name = name[:255]
		}
		itemID := strings.TrimSpace(item.ReferenceID)
		if itemID == "" {
			itemID = "item-" + strconv.Itoa(index+1)
		}
		entry := map[string]any{"id": itemID, "name": name, "quantity": quantity, "price": price}
		if category := strings.TrimSpace(item.Category); category != "" {
			entry["category"] = category
		}
		result = append(result, entry)
		total += price * int64(quantity)
	}
	if len(result) == 0 || total != expected {
		return nil
	}
	return result
}

func providerItemAmount(amount int64, currency string) (int64, error) {
	if amount <= 0 || !strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return 0, errors.New("invalid DOKU line item amount")
	}
	return amount, nil
}

func paymentDueDate(value string, now time.Time) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return 0, errors.New("expires_at must use RFC3339")
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return 0, errors.New("expires_at must be in the future")
	}
	minutes := int64((remaining + time.Minute - 1) / time.Minute)
	if minutes > 999_999 {
		minutes = 999_999
	}
	return minutes, nil
}

func paymentStatus(transactionStatus, orderStatus string) string {
	switch strings.ToUpper(strings.TrimSpace(transactionStatus)) {
	case "SUCCESS", "REFUNDED":
		return "SUCCEEDED"
	case "EXPIRED":
		return "EXPIRED"
	case "TIMEOUT":
		return "UNKNOWN"
	case "FAILED", "PENDING", "REDIRECT", "":
		// DOKU Checkout lets the customer retry or change method after a
		// channel-level failure, so FAILED is not terminal for the order.
		if strings.EqualFold(strings.TrimSpace(orderStatus), "ORDER_EXPIRED") {
			return "EXPIRED"
		}
		return "PENDING"
	default:
		return "PENDING"
	}
}

func dokuAPIError(status int, response map[string]any) *connector.APIError {
	code := firstString(response, "error_code", "code")
	if code == "" {
		code = "DOKU_ERROR"
	}
	message := firstString(response, "error_message", "message")
	if messages, ok := response["message"].([]any); message == "" && ok {
		for _, item := range messages {
			if value, ok := item.(string); ok && strings.TrimSpace(value) != "" {
				message = strings.TrimSpace(value)
				break
			}
		}
	}
	if message == "" {
		message = "DOKU rejected the request"
	}
	return &connector.APIError{Provider: "doku", Status: status, Code: code, Message: message}
}

func invalidWebhookSignature() *connector.APIError {
	return &connector.APIError{Provider: "doku", Status: http.StatusUnauthorized, Code: "INVALID_WEBHOOK_SIGNATURE"}
}

func nestedMap(input map[string]any, key string) map[string]any {
	value, _ := input[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
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

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func validRequestTarget(value string) bool {
	return strings.HasPrefix(value, "/webhooks/v1/providers/doku/") && !strings.ContainsAny(value, "\r\n?#")
}
