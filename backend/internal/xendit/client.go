package xendit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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
	"unicode"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	maxResponseBytes = 1 << 20
	apiVersion       = "2024-11-11"
)

type Client struct {
	baseURL          *url.URL
	httpClient       *http.Client
	executableSHA256 string
}

// SetExecutableSHA256 binds the connector capability handshake to the exact
// standalone Provider App executable. It is set once during process startup.
func (c *Client) SetExecutableSHA256(value string) {
	c.executableSHA256 = strings.ToLower(strings.TrimSpace(value))
}

func New(baseURL string, timeout time.Duration) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.xendit.co"
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid Xendit base URL")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Xendit redirect is not allowed")
			},
		},
	}, nil
}

func (c *Client) Code() string { return "xendit" }

func (c *Client) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	apiKey := strings.TrimSpace(input.Credentials["api_key"])
	if apiKey == "" {
		return connector.InstallationResult{}, errors.New("Xendit api_key is required")
	}
	environment, err := xenditEnvironment(apiKey)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	if err := c.do(ctx, http.MethodGet, "/balance?currency=IDR", apiKey, "", nil, nil, false); err != nil {
		return connector.InstallationResult{}, err
	}
	webhookToken := strings.TrimSpace(input.Credentials["webhook_verification_token"])
	if webhookToken == "" {
		return connector.InstallationResult{}, errors.New("Xendit webhook_verification_token is required")
	}
	stored := map[string]string{"api_key": apiKey, "webhook_verification_token": webhookToken}
	return connector.InstallationResult{
		ConnectorID:       "xendit:" + input.InstallationID,
		Environment:       environment,
		StoredCredentials: stored,
		WebhookReady:      strings.TrimSpace(input.PublicWebhookURL) != "",
	}, nil
}

func xenditEnvironment(apiKey string) (string, error) {
	switch {
	case strings.HasPrefix(apiKey, "xnd_development_"):
		return "sandbox", nil
	case strings.HasPrefix(apiKey, "xnd_production_"):
		return "live", nil
	default:
		return "", fmt.Errorf("%w: Xendit Secret API Key must identify a test or live account", connector.ErrInvalidCredential)
	}
}

func (c *Client) DisableInstallation(context.Context, connector.InstallationInput) error {
	return nil
}

func (c *Client) CreatePayment(ctx context.Context, input connector.PaymentInput) (connector.PaymentResult, error) {
	key, err := apiKey(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	amount, err := providerAmount(input.Amount, input.Currency)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if input.CheckoutMode == connector.CheckoutModeProviderHosted {
		allowedChannels, channelErr := c.hostedPaymentChannels(input.AllowedPaymentMethods)
		if channelErr != nil {
			return connector.PaymentResult{}, channelErr
		}
		return c.createHostedPaymentSession(ctx, input, key, amount, allowedChannels)
	}
	if input.PaymentMethodCode == "card" {
		return c.createHostedPaymentSession(ctx, input, key, amount, []string{"CARDS"})
	}
	channelCode := strings.TrimSpace(input.ChannelCode)
	channelProperties := map[string]any{}
	switch {
	case input.PaymentMethodCode == "qris":
		if channelCode == "" {
			channelCode = "QRIS"
		}
	case strings.HasPrefix(input.PaymentMethodCode, "va_"):
		if channelCode == "" {
			if input.PaymentMethodCode != "va_bca" {
				return connector.PaymentResult{}, connector.ErrNotSupported
			}
			channelCode = "BCA_VIRTUAL_ACCOUNT"
		}
		name := strings.TrimSpace(input.Customer.Name)
		if name == "" {
			name = "Emisell Customer"
		}
		channelProperties["display_name"] = name
		if strings.TrimSpace(input.ExpiresAt) != "" {
			channelProperties["expires_at"] = strings.TrimSpace(input.ExpiresAt)
		}
		if strings.TrimSpace(input.ReturnURL) != "" {
			channelProperties["success_return_url"] = strings.TrimSpace(input.ReturnURL)
		}
	case strings.HasPrefix(input.PaymentMethodCode, "ewallet_"):
		if channelCode == "" {
			return connector.PaymentResult{}, connector.ErrNotSupported
		}
		returnURL := strings.TrimSpace(input.ReturnURL)
		if returnURL == "" {
			return connector.PaymentResult{}, errors.New("return_url is required for Xendit e-wallet payments")
		}
		channelProperties["success_return_url"] = returnURL
		channelProperties["failure_return_url"] = returnURL
		if strings.EqualFold(channelCode, "OVO") {
			phone := strings.TrimSpace(input.Customer.Phone)
			if phone == "" {
				return connector.PaymentResult{}, errors.New("customer.phone is required for Xendit OVO payments")
			}
			channelProperties["account_mobile_number"] = phone
		}
	case strings.HasPrefix(input.PaymentMethodCode, "paylater_") || input.PaymentMethodCode == "digital_banking_jenius":
		if channelCode == "" {
			return connector.PaymentResult{}, connector.ErrNotSupported
		}
		returnURL := strings.TrimSpace(input.ReturnURL)
		if returnURL == "" {
			return connector.PaymentResult{}, errors.New("return_url is required for Xendit redirect payments")
		}
		channelProperties["success_return_url"] = returnURL
		channelProperties["failure_return_url"] = returnURL
	default:
		return connector.PaymentResult{}, connector.ErrNotSupported
	}
	payload := map[string]any{
		"reference_id":   input.MerchantReference,
		"type":           "PAY",
		"country":        "ID",
		"currency":       input.Currency,
		"request_amount": amount,
		"channel_code":   channelCode,
		"capture_method": "AUTOMATIC",
		"metadata":       input.Metadata,
	}
	if len(channelProperties) > 0 {
		payload["channel_properties"] = channelProperties
	}
	if customer := xenditCustomer(input); customer != nil {
		payload["customer"] = customer
	}
	items, err := xenditItems(input.Items, input.Currency)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if len(items) > 0 {
		payload["items"] = items
	}
	if strings.TrimSpace(input.Description) != "" {
		payload["description"] = strings.TrimSpace(input.Description)
	}
	var response map[string]any
	if err = c.do(ctx, http.MethodPost, "/v3/payment_requests", key, input.IdempotencyKey, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	result, err := paymentResult(response, channelCode)
	if err != nil {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: err}
	}
	return result, nil
}

func (c *Client) hostedPaymentChannels(methods []connector.PaymentMethodMapping) ([]string, error) {
	if len(methods) == 0 {
		return nil, errors.New("at least one active payment method is required for Xendit hosted checkout")
	}
	channels := make([]string, 0, len(methods))
	seen := make(map[string]struct{}, len(methods))
	for _, method := range methods {
		if err := c.ValidatePaymentMethod(method); err != nil {
			return nil, err
		}
		channelCode := strings.ToUpper(strings.TrimSpace(method.ProviderChannelCode))
		if channelCode == "" {
			return nil, fmt.Errorf("payment method %q has no Xendit channel mapping", method.PaymentMethodCode)
		}
		if _, exists := seen[channelCode]; exists {
			continue
		}
		seen[channelCode] = struct{}{}
		channels = append(channels, channelCode)
	}
	return channels, nil
}

func (c *Client) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	key, err := apiKey(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	var response map[string]any
	path := "/v3/payment_requests/" + url.PathEscape(input.PaymentID)
	isSession := strings.HasPrefix(input.PaymentID, "ps-")
	if isSession {
		path = "/sessions/" + url.PathEscape(input.PaymentID)
	}
	if err = c.do(ctx, http.MethodGet, path, key, "", nil, &response, false); err != nil {
		return connector.PaymentResult{}, err
	}
	if isSession {
		return paymentSessionResult(response)
	}
	return paymentResult(response, firstString(response, "channel_code"))
}

func (c *Client) CapturePayment(context.Context, connector.CaptureInput) (connector.PaymentResult, error) {
	return connector.PaymentResult{}, connector.ErrNotSupported
}

func (c *Client) CancelPayment(context.Context, connector.PaymentLookup, string, string) (connector.PaymentResult, error) {
	return connector.PaymentResult{}, connector.ErrNotSupported
}

func (c *Client) SimulatePayment(ctx context.Context, input connector.PaymentLookup, amount int64, currency string) error {
	if strings.HasPrefix(input.PaymentID, "ps-") {
		return connector.ErrNotSupported
	}
	key, err := apiKey(input.Credentials)
	if err != nil {
		return err
	}
	value, err := providerAmount(amount, currency)
	if err != nil {
		return err
	}
	path := "/v3/payment_requests/" + url.PathEscape(input.PaymentID) + "/simulate"
	return c.do(ctx, http.MethodPost, path, key, "simulate-"+input.PaymentID, map[string]any{"amount": value}, nil, true)
}

func (c *Client) CreateRefund(ctx context.Context, input connector.RefundInput) (connector.RefundResult, error) {
	key, err := apiKey(input.Credentials)
	if err != nil {
		return connector.RefundResult{}, err
	}
	paymentRequestID := strings.TrimSpace(input.PaymentID)
	if !strings.HasPrefix(paymentRequestID, "pr-") {
		return connector.RefundResult{}, &connector.APIError{Provider: "xendit", Status: http.StatusUnprocessableEntity, Code: "REFUND_PAYMENT_REQUEST_REQUIRED", Message: "Xendit Unified Refund requires the original payment_request_id"}
	}
	referenceID := strings.TrimSpace(input.IdempotencyKey)
	if referenceID == "" {
		return connector.RefundResult{}, errors.New("Xendit refund idempotency_key is required")
	}
	amount, err := providerAmount(input.Amount, input.Currency)
	if err != nil {
		return connector.RefundResult{}, err
	}
	reason, err := xenditRefundReason(input.Reason)
	if err != nil {
		return connector.RefundResult{}, err
	}
	payload := map[string]any{
		"reference_id":       referenceID,
		"payment_request_id": paymentRequestID,
		"amount":             amount,
		"currency":           strings.ToUpper(strings.TrimSpace(input.Currency)),
		"reason":             reason,
	}
	if len(input.Metadata) > 0 {
		payload["metadata"] = input.Metadata
	}
	var response map[string]any
	if err = c.do(ctx, http.MethodPost, "/refunds", key, referenceID, payload, &response, true); err != nil {
		return connector.RefundResult{}, err
	}
	refundID := firstString(response, "id", "refund_id")
	if refundID == "" {
		return connector.RefundResult{}, &connector.UnknownOutcomeError{Cause: errors.New("Xendit refund response did not contain an ID")}
	}
	status := firstString(response, "status")
	if status == "" {
		status = "PENDING"
	}
	return connector.RefundResult{ID: refundID, Status: status}, nil
}

func (c *Client) GetRefund(context.Context, connector.RefundLookup) (connector.RefundResult, error) {
	return connector.RefundResult{}, connector.ErrNotSupported
}

func (c *Client) HandleWebhook(_ context.Context, input connector.WebhookInput) (connector.WebhookEvent, error) {
	token := strings.TrimSpace(input.Credentials["webhook_verification_token"])
	provided := strings.TrimSpace(input.Headers.Get("x-callback-token"))
	if token == "" || provided == "" || !hmac.Equal([]byte(token), []byte(provided)) {
		return connector.WebhookEvent{}, &connector.APIError{Provider: "xendit", Status: http.StatusUnauthorized, Code: "INVALID_WEBHOOK_SIGNATURE"}
	}
	var payload map[string]any
	if err := json.Unmarshal(input.Body, &payload); err != nil {
		return connector.WebhookEvent{}, err
	}
	eventType := firstString(payload, "event", "type")
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = payload
	}
	eventID := strings.TrimSpace(input.Headers.Get("webhook-id"))
	if eventID == "" {
		eventID = firstString(payload, "id")
	}
	if eventID == "" {
		sum := sha256.Sum256(input.Body)
		eventID = hex.EncodeToString(sum[:])
	}
	paymentID := firstString(data, "payment_request_id")
	if strings.HasPrefix(strings.ToLower(eventType), "payment_session.") {
		paymentID = firstString(data, "payment_session_id")
	}
	refundID := firstString(data, "refund_id")
	if paymentID == "" && strings.HasPrefix(strings.ToLower(eventType), "payment_request.") {
		paymentID = firstString(data, "id")
	}
	if refundID == "" && strings.HasPrefix(strings.ToLower(eventType), "refund.") {
		refundID = firstString(data, "id")
	}
	status := firstString(data, "status")
	if status == "" {
		status = webhookStatus(eventType)
	}
	return connector.WebhookEvent{ID: eventID, Type: eventType, PaymentID: paymentID, RefundID: refundID, Status: status}, nil
}

func (c *Client) createHostedPaymentSession(ctx context.Context, input connector.PaymentInput, key string, amount any, allowedChannels []string) (connector.PaymentResult, error) {
	returnURL := strings.TrimSpace(input.ReturnURL)
	if !isHTTPSURL(returnURL) {
		return connector.PaymentResult{}, errors.New("https return_url is required for Xendit hosted checkout")
	}
	reference := strings.TrimSpace(input.MerchantReference)
	if reference == "" || len(reference) > 64 {
		return connector.PaymentResult{}, errors.New("merchant_reference must be between 1 and 64 characters for Xendit Payment Session")
	}
	payload := map[string]any{
		"reference_id":              reference,
		"session_type":              "PAY",
		"mode":                      "PAYMENT_LINK",
		"capture_method":            "AUTOMATIC",
		"allow_save_payment_method": "DISABLED",
		"country":                   "ID",
		"currency":                  strings.ToUpper(strings.TrimSpace(input.Currency)),
		"amount":                    amount,
		"success_return_url":        returnURL,
		"cancel_return_url":         returnURL,
		"description":               "Emisell payment",
		"metadata":                  input.Metadata,
	}
	if len(allowedChannels) > 0 {
		payload["allowed_payment_channels"] = allowedChannels
	}
	if expiresAt := strings.TrimSpace(input.ExpiresAt); expiresAt != "" {
		payload["expires_at"] = expiresAt
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		payload["description"] = description
	}
	if customer := xenditCustomer(input); customer != nil {
		payload["customer"] = customer
	}
	items, err := xenditSessionItems(input.Items, input.Currency)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if len(items) > 0 {
		payload["items"] = items
	}
	var response map[string]any
	if err = c.do(ctx, http.MethodPost, "/sessions", key, input.IdempotencyKey, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	result, err := paymentSessionResult(response)
	if err != nil {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: err}
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, method, path, key, idempotencyKey string, payload, target any, unknownOnTransport bool) error {
	parts := strings.SplitN(path, "?", 2)
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: parts[0]})
	if len(parts) == 2 {
		endpoint.RawQuery = parts[1]
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Emisell-Payment-Engine/1.0")
	request.Header.Set("api-version", apiVersion)
	request.SetBasicAuth(key, "")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("idempotency-key", idempotencyKey)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		if unknownOnTransport {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if unknownOnTransport {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	if len(responseBytes) > maxResponseBytes {
		return errors.New("Xendit response exceeds 1 MiB")
	}
	var decoded map[string]any
	if len(responseBytes) > 0 {
		_ = json.Unmarshal(responseBytes, &decoded)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		code := firstString(decoded, "error_code", "code")
		if code == "" {
			code = "XENDIT_ERROR"
		}
		apiErr := &connector.APIError{Provider: "xendit", Status: response.StatusCode, Code: code, Message: providerErrorMessage(decoded)}
		if unknownOnTransport && (response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError) {
			return &connector.UnknownOutcomeError{Cause: apiErr}
		}
		return apiErr
	}
	if target != nil && len(responseBytes) > 0 {
		if err = json.Unmarshal(responseBytes, target); err != nil {
			if unknownOnTransport {
				return &connector.UnknownOutcomeError{Cause: err}
			}
			return err
		}
	}
	return nil
}

func paymentResult(response map[string]any, channelCode string) (connector.PaymentResult, error) {
	id := firstString(response, "id", "payment_request_id")
	if id == "" {
		return connector.PaymentResult{}, errors.New("Xendit payment response did not contain an ID")
	}
	result := connector.PaymentResult{
		ID:                     id,
		Status:                 firstString(response, "status"),
		ConnectorTransactionID: firstString(response, "payment_id"),
	}
	if result.ConnectorTransactionID == "" {
		result.ConnectorTransactionID = id
	}
	actions, _ := response["actions"].([]any)
	nextAction := normalizeActions(actions, channelCode)
	if len(actions) > 0 || firstString(nextAction, "type") != "provider_actions" {
		result.NextAction, _ = json.Marshal(nextAction)
	}
	return result, nil
}

func paymentSessionResult(response map[string]any) (connector.PaymentResult, error) {
	id := firstString(response, "payment_session_id")
	if id == "" {
		return connector.PaymentResult{}, errors.New("Xendit Payment Session response did not contain an ID")
	}
	status := strings.ToUpper(firstString(response, "status"))
	switch status {
	case "ACTIVE":
		status = "REQUIRES_ACTION"
	case "COMPLETED":
		status = "SUCCEEDED"
	case "CANCELED":
		status = "CANCELLED"
	}
	connectorID := firstString(response, "payment_id", "payment_request_id")
	if connectorID == "" {
		connectorID = id
	}
	result := connector.PaymentResult{ID: id, Status: status, ConnectorTransactionID: connectorID}
	if paymentLink := firstString(response, "payment_link_url"); isHTTPSURL(paymentLink) {
		result.NextAction, _ = json.Marshal(map[string]any{
			"type":         "redirect",
			"redirect_url": paymentLink,
			"actions": []map[string]any{{
				"type": "REDIRECT_CUSTOMER", "descriptor": "WEB_URL", "value": paymentLink,
			}},
		})
	}
	return result, nil
}

func normalizeActions(actions []any, channelCode string) map[string]any {
	result := map[string]any{"type": "provider_actions", "actions": actions}
	if len(actions) == 0 && strings.EqualFold(channelCode, "OVO") {
		result["type"] = "mobile_authorization"
		result["display_text"] = "Approve the payment in the OVO application"
		return result
	}
	for _, value := range actions {
		action, ok := value.(map[string]any)
		if !ok {
			continue
		}
		actionType := strings.ToUpper(firstString(action, "type", "action"))
		descriptor := strings.ToUpper(firstString(action, "descriptor", "url_type"))
		actionValue := firstString(action, "value", "url", "qr_code")
		if actionValue == "" {
			continue
		}
		if strings.Contains(descriptor, "QR") || strings.HasPrefix(actionValue, "000201") || strings.EqualFold(channelCode, "QRIS") {
			if firstString(result, "type") == "redirect" && !strings.EqualFold(channelCode, "QRIS") {
				continue
			}
			result["type"] = "qr_code_information"
			result["raw_qr_data"] = actionValue
			if strings.EqualFold(channelCode, "QRIS") {
				break
			}
			continue
		}
		if strings.Contains(descriptor, "VIRTUAL_ACCOUNT") || strings.Contains(strings.ToUpper(channelCode), "VIRTUAL_ACCOUNT") {
			result["type"] = "virtual_account_information"
			result["display_text"] = actionValue
			break
		}
		if actionType == "REDIRECT_CUSTOMER" || strings.HasPrefix(actionValue, "https://") {
			result["type"] = "redirect"
			result["redirect_url"] = actionValue
		}
	}
	return result
}

func xenditCustomer(input connector.PaymentInput) map[string]any {
	name := safeCustomerName(input.Customer.Name)
	if name == "" && strings.TrimSpace(input.Customer.Email) == "" && strings.TrimSpace(input.Customer.Phone) == "" {
		return nil
	}
	if name == "" {
		name = "Emisell Customer"
	}
	reference := alphanumeric(input.LocalPaymentID)
	if reference == "" {
		reference = alphanumeric(input.MerchantReference)
	}
	if reference == "" {
		reference = "emisellcustomer"
	}
	if len(reference) > 240 {
		reference = reference[:240]
	}
	result := map[string]any{
		"reference_id": "cust" + reference,
		"type":         "INDIVIDUAL",
		"individual_detail": map[string]any{
			"given_names": name,
		},
	}
	if email := strings.TrimSpace(input.Customer.Email); email != "" {
		result["email"] = email
	}
	if phone := strings.TrimSpace(input.Customer.Phone); phone != "" {
		result["mobile_number"] = phone
	}
	return result
}

func xenditItems(input []connector.Item, currency string) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(input))
	for index, item := range input {
		amount, err := providerAmount(item.NetUnitAmount, currency)
		if err != nil {
			return nil, err
		}
		quantity := item.Quantity
		if quantity < 1 {
			quantity = 1
		}
		reference := strings.TrimSpace(item.ReferenceID)
		if reference == "" {
			reference = "item" + strconv.Itoa(index+1)
		}
		itemType := strings.ToUpper(strings.TrimSpace(item.Type))
		if itemType == "" {
			itemType = "DIGITAL_PRODUCT"
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = "Emisell item"
		}
		category := strings.TrimSpace(item.Category)
		if category == "" {
			category = "general"
		}
		result = append(result, map[string]any{
			"reference_id":    reference,
			"type":            itemType,
			"name":            name,
			"net_unit_amount": amount,
			"quantity":        quantity,
			"currency":        strings.ToUpper(strings.TrimSpace(currency)),
			"category":        category,
		})
	}
	return result, nil
}

func xenditSessionItems(input []connector.Item, currency string) ([]map[string]any, error) {
	items, err := xenditItems(input, currency)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		delete(item, "currency")
	}
	return items, nil
}

func safeCustomerName(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 50 {
		value = value[:50]
	}
	return value
}

func alphanumeric(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func apiKey(credentials map[string]string) (string, error) {
	key := strings.TrimSpace(credentials["api_key"])
	if key == "" {
		return "", errors.New("Xendit api_key is required")
	}
	return key, nil
}

func providerAmount(amount int64, currency string) (any, error) {
	if amount <= 0 {
		return nil, errors.New("payment amount must be positive")
	}
	if strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return amount, nil
	}
	return nil, errors.New("Xendit connector only supports IDR")
}

func xenditRefundReason(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "REQUESTED_BY_CUSTOMER", "CANCELLATION", "DUPLICATE", "FRAUDULENT", "OTHERS":
		return strings.ToUpper(strings.TrimSpace(value)), nil
	default:
		return "", errors.New("Xendit refund reason is invalid")
	}
}

func webhookStatus(eventType string) string {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "payment.capture", "payment.succeeded", "payment_session.completed", "refund.succeeded":
		return "SUCCEEDED"
	case "payment.authorization":
		return "AUTHORIZED"
	case "payment.failure", "payment.failed", "refund.failed":
		return "FAILED"
	case "payment.expiry", "payment_request.expiry", "payment_session.expired":
		return "EXPIRED"
	default:
		return "PENDING"
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerErrorMessage(values map[string]any) string {
	parts := []string{}
	if message := firstString(values, "message"); message != "" {
		parts = append(parts, message)
	}
	if details, ok := values["errors"].([]any); ok {
		for _, detail := range details {
			item, ok := detail.(map[string]any)
			if !ok {
				continue
			}
			message := firstString(item, "message", "reason")
			if message != "" && !containsString(parts, message) {
				parts = append(parts, message)
			}
		}
	}
	return strings.Join(parts, "; ")
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
