package ipaymu

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
	"net/mail"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

const (
	maxRequestBytes     = 64 << 10
	maxResponseBytes    = 1 << 20
	paymentPath         = "/api/v2/payment"
	directPaymentPath   = "/api/v2/payment/direct"
	transactionPath     = "/api/v2/transaction"
	paymentChannelsPath = "/api/v2/payment-channels"
)

type Client struct {
	sandboxURL       *url.URL
	liveURL          *url.URL
	httpClient       *http.Client
	executableSHA256 string
	now              func() time.Time
}

type apiCredentials struct {
	va     string
	apiKey string
}

func New(sandboxBaseURL, liveBaseURL string, timeout time.Duration) (*Client, error) {
	sandbox, err := parseBaseURL(sandboxBaseURL, "https://sandbox.ipaymu.com")
	if err != nil {
		return nil, fmt.Errorf("invalid iPaymu sandbox base URL: %w", err)
	}
	live, err := parseBaseURL(liveBaseURL, "https://my.ipaymu.com")
	if err != nil {
		return nil, fmt.Errorf("invalid iPaymu live base URL: %w", err)
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
				return errors.New("iPaymu redirect is not allowed")
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

func (c *Client) Code() string { return "ipaymu" }

func (c *Client) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	environment := strings.ToLower(strings.TrimSpace(input.Environment))
	if _, err = c.baseURL(environment); err != nil {
		return connector.InstallationResult{}, err
	}
	var response map[string]any
	if err = c.do(ctx, environment, http.MethodGet, paymentChannelsPath, credentials, nil, &response, false); err != nil {
		return connector.InstallationResult{}, fmt.Errorf("%w: %v", connector.ErrInvalidCredential, err)
	}
	if status := firstInt64(response, "Status", "status"); status != 0 && status != http.StatusOK {
		return connector.InstallationResult{}, fmt.Errorf("%w: iPaymu returned status %d", connector.ErrInvalidCredential, status)
	}
	return connector.InstallationResult{
		ConnectorID:  "ipaymu:" + input.InstallationID,
		Environment:  environment,
		WebhookReady: isHTTPSURL(input.PublicWebhookURL),
		StoredCredentials: map[string]string{
			"va":      credentials.va,
			"api_key": credentials.apiKey,
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
	if !isHTTPSURL(input.PublicWebhookURL) {
		return connector.PaymentResult{}, errors.New("the installation webhook URL must be public HTTPS for iPaymu")
	}
	referenceID := paymentReference(input)
	if input.CheckoutMode == connector.CheckoutModeProviderHosted {
		return c.createHostedPayment(ctx, input, credentials, amount, referenceID)
	}
	return c.createDirectPayment(ctx, input, credentials, amount, referenceID)
}

func (c *Client) createHostedPayment(ctx context.Context, input connector.PaymentInput, credentials apiCredentials, amount int64, referenceID string) (connector.PaymentResult, error) {
	if !isHTTPSURL(input.ReturnURL) {
		return connector.PaymentResult{}, errors.New("return_url must be a public HTTPS URL for iPaymu hosted checkout")
	}
	products, quantities, prices, descriptions := redirectItems(input.Items, input.Currency, amount, input.Description)
	payload := map[string]any{
		"product":     products,
		"qty":         quantities,
		"price":       prices,
		"description": descriptions,
		"returnUrl":   strings.TrimSpace(input.ReturnURL),
		"notifyUrl":   strings.TrimSpace(input.PublicWebhookURL),
		"cancelUrl":   strings.TrimSpace(input.ReturnURL),
		"referenceId": referenceID,
	}
	addBuyer(payload, input.Customer)
	if expiry, expiryErr := expiryHours(input.ExpiresAt, c.now()); expiryErr != nil {
		return connector.PaymentResult{}, expiryErr
	} else if expiry > 0 {
		payload["expired"] = expiry
	}
	var response map[string]any
	if err := c.do(ctx, input.Environment, http.MethodPost, paymentPath, credentials, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	data := nestedMap(response, "Data", "data")
	redirectURL := firstString(data, "Url", "URL", "url")
	if !isHTTPSURL(redirectURL) {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: errors.New("iPaymu response did not contain a valid Data.Url")}
	}
	nextAction, _ := json.Marshal(map[string]any{"type": "redirect", "redirect_url": redirectURL})
	return connector.PaymentResult{
		ID: referenceID, Status: "REQUIRES_ACTION",
		ConnectorTransactionID: firstString(data, "SessionID", "SessionId", "sessionId", "session_id"),
		NextAction:             nextAction,
	}, nil
}

func (c *Client) createDirectPayment(ctx context.Context, input connector.PaymentInput, credentials apiCredentials, amount int64, referenceID string) (connector.PaymentResult, error) {
	mapping, err := ipaymuChannel(input.PaymentMethodCode, input.ChannelCode)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	name, email, phone, err := directBuyer(input.Customer)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	payload := map[string]any{
		"name":           name,
		"phone":          phone,
		"email":          email,
		"amount":         amount,
		"notifyUrl":      strings.TrimSpace(input.PublicWebhookURL),
		"referenceId":    referenceID,
		"paymentMethod":  mapping.paymentMethod,
		"paymentChannel": mapping.channelCode,
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		payload["comments"] = truncate(description, 255)
	}
	if isHTTPSURL(input.ReturnURL) {
		payload["successUrl"] = strings.TrimSpace(input.ReturnURL)
		payload["cancelUrl"] = strings.TrimSpace(input.ReturnURL)
	}
	if expiry, expiryErr := directExpiryHours(input.ExpiresAt, mapping.channelCode, c.now()); expiryErr != nil {
		return connector.PaymentResult{}, expiryErr
	} else if expiry > 0 {
		payload["expired"] = expiry
	}
	var response map[string]any
	if err = c.do(ctx, input.Environment, http.MethodPost, directPaymentPath, credentials, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	data := nestedMap(response, "Data", "data")
	nextAction := directNextAction(mapping, data)
	if nextAction == nil {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: errors.New("iPaymu direct payment response did not contain a customer action")}
	}
	nextActionJSON, _ := json.Marshal(nextAction)
	return connector.PaymentResult{
		ID: referenceID, Status: "REQUIRES_ACTION",
		ConnectorTransactionID: firstString(data, "TransactionId", "TransactionID", "transactionId", "SessionId", "SessionID"),
		NextAction:             nextActionJSON,
	}, nil
}

func (c *Client) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	referenceID := strings.TrimSpace(input.PaymentID)
	if referenceID == "" {
		return connector.PaymentResult{}, errors.New("iPaymu payment_id is required")
	}
	payload := map[string]any{"referenceId": referenceID}
	var response map[string]any
	if err = c.do(ctx, input.Environment, http.MethodPost, transactionPath, credentials, payload, &response, false); err != nil {
		return connector.PaymentResult{}, err
	}
	data := nestedMap(response, "Data", "data")
	return connector.PaymentResult{
		ID:                     referenceID,
		Status:                 paymentStatus(firstInt64(data, "Status", "status"), firstString(data, "StatusDesc", "statusDesc", "PaidStatus", "paidStatus")),
		ConnectorTransactionID: firstString(data, "TransactionId", "TransactionID", "transactionId", "SessionId", "SessionID"),
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
	provided := strings.ToLower(strings.TrimSpace(input.Headers.Get("X-Signature")))
	externalID := strings.TrimSpace(input.Headers.Get("X-External-ID"))
	timestamp := strings.TrimSpace(input.Headers.Get("X-Timestamp"))
	if provided == "" || externalID == "" || timestamp == "" {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	payload, err := normalizedWebhookPayload(input.Headers.Get("Content-Type"), input.Body)
	if err != nil {
		return connector.WebhookEvent{}, err
	}
	expected, err := webhookSignature(payload, credentials.va)
	if err != nil || !hmac.Equal([]byte(provided), []byte(expected)) {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	if merchant := firstString(payload, "merchant"); merchant != "" && merchant != credentials.va {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	referenceID := firstString(payload, "reference_id", "referenceId")
	if referenceID == "" {
		return connector.WebhookEvent{}, errors.New("iPaymu callback is missing reference_id")
	}
	return connector.WebhookEvent{
		ID: externalID, Type: "payment.updated", PaymentID: referenceID,
		Status: paymentStatus(firstInt64(payload, "status_code", "transaction_status_code"), firstString(payload, "status", "status_desc")),
	}, nil
}

func (c *Client) do(ctx context.Context, environment, method, endpointPath string, credentials apiCredentials, payload, target any, mutation bool) error {
	baseURL, err := c.baseURL(environment)
	if err != nil {
		return err
	}
	bodyBytes := []byte("{}")
	if payload != nil {
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	if len(bodyBytes) > maxRequestBytes {
		return errors.New("iPaymu request exceeds 64 KiB")
	}
	endpoint := *baseURL
	endpoint.Path = path.Join(baseURL.Path, endpointPath)
	var body io.Reader
	if method != http.MethodGet {
		body = bytes.NewReader(bodyBytes)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Emisell-Connector-Runner/1.0")
	request.Header.Set("va", credentials.va)
	request.Header.Set("signature", requestSignature(method, credentials, bodyBytes))
	request.Header.Set("timestamp", c.now().Format("20060102150405"))
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
		err = errors.New("iPaymu response exceeds 1 MiB")
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	decoded := map[string]any{}
	if len(responseBytes) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(responseBytes))
		decoder.UseNumber()
		_ = decoder.Decode(&decoded)
	}
	providerStatus := firstInt64(decoded, "Status", "status")
	if response.StatusCode < 200 || response.StatusCode >= 300 || (providerStatus != 0 && providerStatus != http.StatusOK) {
		errorStatus := response.StatusCode
		if errorStatus >= 200 && errorStatus < 300 {
			errorStatus = http.StatusBadRequest
		}
		apiErr := ipaymuAPIError(errorStatus, decoded)
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
		return nil, errors.New("iPaymu environment must be sandbox or live")
	}
}

func parseCredentials(input map[string]string) (apiCredentials, error) {
	credentials := apiCredentials{va: strings.TrimSpace(input["va"]), apiKey: strings.TrimSpace(input["api_key"])}
	if credentials.va == "" || len(credentials.va) > 64 || strings.ContainsAny(credentials.va, "\r\n") {
		return apiCredentials{}, errors.New("iPaymu va is required and must be valid")
	}
	if credentials.apiKey == "" || len(credentials.apiKey) > 512 || strings.ContainsAny(credentials.apiKey, "\r\n") {
		return apiCredentials{}, errors.New("iPaymu api_key is required and must be valid")
	}
	return credentials, nil
}

func requestSignature(method string, credentials apiCredentials, body []byte) string {
	bodyHash := sha256.Sum256(body)
	stringToSign := strings.ToUpper(method) + ":" + credentials.va + ":" + hex.EncodeToString(bodyHash[:]) + ":" + credentials.apiKey
	digest := hmac.New(sha256.New, []byte(credentials.apiKey))
	_, _ = digest.Write([]byte(stringToSign))
	return hex.EncodeToString(digest.Sum(nil))
}

func providerAmount(amount int64, currency string) (int64, error) {
	if amount <= 0 {
		return 0, errors.New("payment amount must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return 0, errors.New("iPaymu connector supports IDR only")
	}
	return amount, nil
}

func paymentReference(input connector.PaymentInput) string {
	value := strings.TrimSpace(input.MerchantReference)
	if value == "" {
		value = strings.TrimSpace(input.LocalPaymentID)
	}
	if value == "" {
		value = strings.TrimSpace(input.IdempotencyKey)
	}
	value = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-._")
	if value == "" {
		value = "emisell-payment"
	}
	if len(value) > 64 {
		digest := sha256.Sum256([]byte(value))
		value = value[:47] + "-" + hex.EncodeToString(digest[:])[:16]
	}
	return value
}

func redirectItems(items []connector.Item, currency string, expected int64, fallbackDescription string) ([]string, []int, []int64, []string) {
	products := make([]string, 0, len(items))
	quantities := make([]int, 0, len(items))
	prices := make([]int64, 0, len(items))
	descriptions := make([]string, 0, len(items))
	var total int64
	for _, item := range items {
		price, err := providerAmount(item.NetUnitAmount, currency)
		if err != nil {
			return fallbackItem(expected, fallbackDescription)
		}
		quantity := item.Quantity
		if quantity < 1 {
			quantity = 1
		}
		name := truncate(strings.TrimSpace(item.Name), 255)
		if name == "" {
			name = "Emisell item"
		}
		description := truncate(strings.TrimSpace(item.Category), 255)
		if description == "" {
			description = name
		}
		products = append(products, name)
		quantities = append(quantities, quantity)
		prices = append(prices, price)
		descriptions = append(descriptions, description)
		total += price * int64(quantity)
	}
	if len(products) == 0 || total != expected {
		return fallbackItem(expected, fallbackDescription)
	}
	return products, quantities, prices, descriptions
}

func fallbackItem(amount int64, description string) ([]string, []int, []int64, []string) {
	description = truncate(strings.TrimSpace(description), 255)
	if description == "" {
		description = "Emisell payment"
	}
	return []string{description}, []int{1}, []int64{amount}, []string{description}
}

func addBuyer(payload map[string]any, customer connector.Customer) {
	if name := strings.TrimSpace(customer.Name); name != "" {
		payload["buyerName"] = truncate(name, 255)
	}
	if email := strings.TrimSpace(customer.Email); email != "" {
		payload["buyerEmail"] = truncate(email, 254)
	}
	if phone := strings.TrimSpace(customer.Phone); phone != "" {
		payload["buyerPhone"] = truncate(phone, 32)
	}
}

func directBuyer(customer connector.Customer) (string, string, string, error) {
	name := truncate(strings.TrimSpace(customer.Name), 255)
	email := strings.TrimSpace(customer.Email)
	phone := truncate(strings.TrimSpace(customer.Phone), 32)
	if name == "" || email == "" || phone == "" {
		return "", "", "", errors.New("customer name, email, and phone are required for iPaymu direct payment")
	}
	address, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(address.Address, email) || len(email) > 254 {
		return "", "", "", errors.New("customer email must be valid for iPaymu direct payment")
	}
	return name, email, phone, nil
}

func expiryHours(value string, now time.Time) (int64, error) {
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
	hours := int64((remaining + time.Hour - 1) / time.Hour)
	if hours > 720 {
		hours = 720
	}
	return hours, nil
}

func directExpiryHours(value, channel string, now time.Time) (int64, error) {
	if channel == "bca" || channel == "alfamart" || channel == "mpm" {
		return 0, nil
	}
	hours, err := expiryHours(value, now)
	if err != nil || hours == 0 {
		return hours, err
	}
	limit := int64(0)
	if channel == "bri" {
		limit = 2
	} else if channel == "bsi" {
		limit = 3
	}
	if limit > 0 && hours > limit {
		return 0, fmt.Errorf("iPaymu %s expiry cannot exceed %d hours", strings.ToUpper(channel), limit)
	}
	return hours, nil
}

func directNextAction(mapping methodMapping, data map[string]any) map[string]any {
	if redirectURL := firstString(data, "Url", "URL", "url"); isHTTPSURL(redirectURL) {
		return map[string]any{"type": "redirect", "redirect_url": redirectURL}
	}
	paymentNumber := firstString(data, "PaymentNo", "paymentNo", "payment_no")
	if mapping.paymentMethod == "va" && paymentNumber != "" {
		return map[string]any{"type": "virtual_account_information", "bank": mapping.channelCode, "va_number": paymentNumber, "display_text": paymentNumber}
	}
	if mapping.paymentMethod == "qris" {
		if qr := firstString(data, "QrString", "QRString", "qrString", "PaymentNo", "paymentNo"); qr != "" {
			return map[string]any{"type": "qr_code_information", "qr_string": qr, "display_text": qr}
		}
	}
	if paymentNumber != "" {
		return map[string]any{"type": "provider_actions", "payment_method": mapping.paymentMethod, "payment_channel": mapping.channelCode, "payment_number": paymentNumber}
	}
	return nil
}

func paymentStatus(status int64, description string) string {
	switch status {
	case 1, 6:
		return "SUCCEEDED"
	case 2:
		return "CANCELLED"
	case -2:
		return "EXPIRED"
	case 4, 5:
		return "FAILED"
	case 0, 7:
		return "PENDING"
	}
	switch strings.ToLower(strings.TrimSpace(description)) {
	case "berhasil", "success", "paid":
		return "SUCCEEDED"
	case "cancelled", "canceled", "batal":
		return "CANCELLED"
	case "expired", "kadaluarsa":
		return "EXPIRED"
	case "failed", "failure", "gagal", "error":
		return "FAILED"
	default:
		return "PENDING"
	}
}

func normalizedWebhookPayload(contentType string, body []byte) (map[string]any, error) {
	payload := map[string]any{}
	if strings.Contains(strings.ToLower(contentType), "application/x-www-form-urlencoded") || (len(bytes.TrimSpace(body)) > 0 && bytes.TrimSpace(body)[0] != '{') {
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, errors.New("invalid iPaymu callback form")
		}
		for key, value := range values {
			if len(value) > 0 {
				payload[key] = value[0]
			}
		}
	} else {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			return nil, errors.New("invalid iPaymu callback JSON")
		}
	}
	delete(payload, "signature")
	for _, key := range []string{"trx_id", "status_code", "transaction_status_code", "paid_off"} {
		if value, ok := payload[key]; ok {
			parsed, err := webhookInteger(value)
			if err != nil {
				return nil, fmt.Errorf("invalid iPaymu callback %s", key)
			}
			payload[key] = parsed
		}
	}
	if value, ok := payload["is_escrow"]; ok {
		payload["is_escrow"] = webhookBoolean(value)
	}
	if value, ok := payload["additional_info"]; !ok || value == nil || value == "[]" {
		payload["additional_info"] = []any{}
	}
	return payload, nil
}

func webhookInteger(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case float64:
		return int64(typed), nil
	case int:
		return int64(typed), nil
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
	default:
		return 0, errors.New("not an integer")
	}
}

func webhookBoolean(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case json.Number:
		return typed.String() == "1"
	case float64:
		return typed == 1
	case string:
		return typed == "1" || strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func webhookSignature(payload map[string]any, va string) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return "", err
	}
	encoded := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	escaped := strings.ReplaceAll(string(encoded), "/", "\\/")
	digest := hmac.New(sha256.New, []byte(va))
	_, _ = digest.Write([]byte(escaped))
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func ipaymuAPIError(status int, response map[string]any) *connector.APIError {
	providerStatus := firstInt64(response, "Status", "status")
	code := "IPAYMU_ERROR"
	if providerStatus != 0 {
		code = "IPAYMU_" + strconv.FormatInt(providerStatus, 10)
	}
	message := firstString(response, "Message", "message", "Error", "error")
	if message == "" {
		message = "iPaymu rejected the request"
	}
	if status < 100 {
		status = http.StatusBadRequest
	}
	return &connector.APIError{Provider: "ipaymu", Status: status, Code: code, Message: message}
}

func invalidWebhookSignature() *connector.APIError {
	return &connector.APIError{Provider: "ipaymu", Status: http.StatusUnauthorized, Code: "INVALID_WEBHOOK_SIGNATURE"}
}

func nestedMap(input map[string]any, keys ...string) map[string]any {
	for _, key := range keys {
		if value, ok := input[key].(map[string]any); ok && value != nil {
			return value
		}
	}
	return map[string]any{}
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
		case int64:
			return strconv.FormatInt(value, 10)
		case int:
			return strconv.Itoa(value)
		}
	}
	return ""
}

func firstInt64(input map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value, ok := input[key]; ok {
			parsed, err := webhookInteger(value)
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func isHTTPSURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func truncate(value string, limit int) string {
	if len(value) > limit {
		return value[:limit]
	}
	return value
}
