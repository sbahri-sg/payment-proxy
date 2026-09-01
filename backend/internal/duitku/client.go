package duitku

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
	maxRequestBytes  = 32 << 10
	maxResponseBytes = 1 << 20
)

type Client struct {
	sandboxPOPURL    *url.URL
	livePOPURL       *url.URL
	sandboxAPIURL    *url.URL
	liveAPIURL       *url.URL
	httpClient       *http.Client
	executableSHA256 string
	now              func() time.Time
}

type apiCredentials struct {
	merchantCode string
	apiKey       string
}

func New(sandboxPOPURL, livePOPURL, sandboxAPIURL, liveAPIURL string, timeout time.Duration) (*Client, error) {
	sandboxPOP, err := parseBaseURL(sandboxPOPURL, "https://api-sandbox.duitku.com")
	if err != nil {
		return nil, fmt.Errorf("invalid Duitku sandbox POP base URL: %w", err)
	}
	livePOP, err := parseBaseURL(livePOPURL, "https://api-prod.duitku.com")
	if err != nil {
		return nil, fmt.Errorf("invalid Duitku live POP base URL: %w", err)
	}
	sandboxAPI, err := parseBaseURL(sandboxAPIURL, "https://sandbox.duitku.com/webapi")
	if err != nil {
		return nil, fmt.Errorf("invalid Duitku sandbox API base URL: %w", err)
	}
	liveAPI, err := parseBaseURL(liveAPIURL, "https://passport.duitku.com/webapi")
	if err != nil {
		return nil, fmt.Errorf("invalid Duitku live API base URL: %w", err)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		sandboxPOPURL: sandboxPOP,
		livePOPURL:    livePOP,
		sandboxAPIURL: sandboxAPI,
		liveAPIURL:    liveAPI,
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("Duitku redirect is not allowed")
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

func (c *Client) Code() string { return "duitku" }

func (c *Client) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	environment := strings.ToLower(strings.TrimSpace(input.Environment))
	if _, err = c.apiBaseURL(environment); err != nil {
		return connector.InstallationResult{}, err
	}
	amount := int64(10_000)
	datetime := c.now().In(time.FixedZone("WIB", 7*60*60)).Format("2006-01-02 15:04:05")
	payload := map[string]any{
		"merchantcode": credentials.merchantCode,
		"amount":       amount,
		"datetime":     datetime,
		"signature":    sign(credentials.apiKey, credentials.merchantCode+strconv.FormatInt(amount, 10)+datetime),
	}
	var response map[string]any
	if err = c.doJSON(ctx, environment, false, "/api/merchant/paymentmethod/getpaymentmethod", nil, payload, &response, false); err != nil {
		return connector.InstallationResult{}, fmt.Errorf("%w: %v", connector.ErrInvalidCredential, err)
	}
	if code := firstString(response, "responseCode", "statusCode"); code != "" && code != "00" {
		return connector.InstallationResult{}, fmt.Errorf("%w: Duitku returned %s", connector.ErrInvalidCredential, firstString(response, "responseMessage", "statusMessage"))
	}
	return connector.InstallationResult{
		ConnectorID:  "duitku:" + input.InstallationID,
		Environment:  environment,
		WebhookReady: isHTTPSURL(input.PublicWebhookURL),
		StoredCredentials: map[string]string{
			"merchant_code": credentials.merchantCode,
			"api_key":       credentials.apiKey,
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
	orderID, err := merchantOrderID(input)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	email, err := customerEmail(input.Customer.Email)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if !isHTTPSURL(input.ReturnURL) {
		return connector.PaymentResult{}, errors.New("return_url must be a public HTTPS URL for Duitku checkout")
	}
	if !isHTTPSURL(input.PublicWebhookURL) {
		return connector.PaymentResult{}, errors.New("the installation webhook URL must be public HTTPS for Duitku checkout")
	}
	paymentMethod := ""
	if input.CheckoutMode == connector.CheckoutModeProviderHosted {
		paymentMethod, err = c.hostedPaymentMethod(input.AllowedPaymentMethods)
		if err != nil {
			return connector.PaymentResult{}, err
		}
	} else {
		paymentMethod, err = duitkuChannel(input.PaymentMethodCode, input.ChannelCode)
		if err != nil {
			return connector.PaymentResult{}, err
		}
	}
	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = "Emisell payment " + orderID
	}
	if len(description) > 255 {
		description = description[:255]
	}
	payload := map[string]any{
		"merchantOrderId": orderID,
		"paymentAmount":   amount,
		"productDetails":  description,
		"email":           email,
		"paymentMethod":   paymentMethod,
		"returnUrl":       strings.TrimSpace(input.ReturnURL),
		"callbackUrl":     strings.TrimSpace(input.PublicWebhookURL),
		"customerVaName":  customerName(input.Customer.Name),
	}
	if phone := strings.TrimSpace(input.Customer.Phone); phone != "" {
		payload["phoneNumber"] = phone
	}
	if items := itemDetails(input.Items, input.Currency, amount); len(items) > 0 {
		payload["itemDetails"] = items
	}
	if expiry, expiryErr := expiryPeriod(input.ExpiresAt, c.now()); expiryErr != nil {
		return connector.PaymentResult{}, expiryErr
	} else if expiry > 0 {
		payload["expiryPeriod"] = expiry
	}
	timestamp := strconv.FormatInt(c.now().UnixMilli(), 10)
	headers := make(http.Header)
	headers.Set("x-duitku-timestamp", timestamp)
	headers.Set("x-duitku-merchantcode", credentials.merchantCode)
	headers.Set("x-duitku-signature", sign(credentials.apiKey, credentials.merchantCode+timestamp))
	var response map[string]any
	if err = c.doJSON(ctx, input.Environment, true, "/api/merchant/createInvoice", headers, payload, &response, true); err != nil {
		return connector.PaymentResult{}, err
	}
	if code := firstString(response, "statusCode", "responseCode"); code != "" && code != "00" {
		return connector.PaymentResult{}, duitkuAPIError(http.StatusBadRequest, response)
	}
	redirectURL := firstString(response, "paymentUrl", "payment_url")
	if !isHTTPSURL(redirectURL) {
		return connector.PaymentResult{}, &connector.UnknownOutcomeError{Cause: errors.New("Duitku response did not contain a valid paymentUrl")}
	}
	nextAction, _ := json.Marshal(map[string]any{"type": "redirect", "redirect_url": redirectURL})
	reference := firstString(response, "reference")
	return connector.PaymentResult{
		ID: orderID, Status: "REQUIRES_ACTION", ConnectorTransactionID: reference, NextAction: nextAction,
	}, nil
}

func (c *Client) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	credentials, err := parseCredentials(input.Credentials)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	orderID := strings.TrimSpace(input.PaymentID)
	if orderID == "" {
		return connector.PaymentResult{}, errors.New("Duitku payment_id is required")
	}
	payload := map[string]any{
		"merchantCode":    credentials.merchantCode,
		"merchantOrderId": orderID,
		"signature":       sign(credentials.apiKey, credentials.merchantCode+orderID),
	}
	var response map[string]any
	if err = c.doJSON(ctx, input.Environment, false, "/api/merchant/transactionStatus", nil, payload, &response, false); err != nil {
		return connector.PaymentResult{}, err
	}
	return connector.PaymentResult{
		ID:                     orderID,
		Status:                 paymentStatus(firstString(response, "statusCode", "responseCode", "resultCode"), firstString(response, "statusMessage", "responseMessage")),
		ConnectorTransactionID: firstString(response, "reference"),
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
	values, err := url.ParseQuery(string(input.Body))
	if err != nil {
		return connector.WebhookEvent{}, errors.New("invalid Duitku callback form")
	}
	merchantCode := strings.TrimSpace(values.Get("merchantCode"))
	amount := strings.TrimSpace(values.Get("amount"))
	orderID := strings.TrimSpace(values.Get("merchantOrderId"))
	provided := strings.ToLower(strings.TrimSpace(values.Get("signature")))
	if merchantCode == "" || merchantCode != credentials.merchantCode || amount == "" || orderID == "" || provided == "" {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	expected := sign(credentials.apiKey, merchantCode+amount+orderID)
	if !hmac.Equal([]byte(provided), []byte(expected)) {
		return connector.WebhookEvent{}, invalidWebhookSignature()
	}
	resultCode := strings.TrimSpace(values.Get("resultCode"))
	reference := strings.TrimSpace(values.Get("reference"))
	eventID := strings.Trim(reference+":"+resultCode, ":")
	if eventID == "" {
		digest := sha256.Sum256(input.Body)
		eventID = hex.EncodeToString(digest[:])
	}
	return connector.WebhookEvent{
		ID: eventID, Type: "payment.updated", PaymentID: orderID,
		Status: paymentStatus(resultCode, ""),
	}, nil
}

func (c *Client) doJSON(ctx context.Context, environment string, pop bool, endpointPath string, headers http.Header, payload, target any, mutation bool) error {
	var baseURL *url.URL
	var err error
	if pop {
		baseURL, err = c.popBaseURL(environment)
	} else {
		baseURL, err = c.apiBaseURL(environment)
	}
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(encoded) > maxRequestBytes {
		return errors.New("Duitku request exceeds 32 KiB")
	}
	endpoint := *baseURL
	endpoint.Path = path.Join(baseURL.Path, endpointPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	for name, values := range headers {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Emisell-Connector-Runner/1.0")
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
		err = errors.New("Duitku response exceeds 1 MiB")
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	decoded := map[string]any{}
	if len(responseBytes) > 0 {
		_ = json.Unmarshal(responseBytes, &decoded)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := duitkuAPIError(response.StatusCode, decoded)
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

func (c *Client) popBaseURL(environment string) (*url.URL, error) {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "sandbox":
		return c.sandboxPOPURL, nil
	case "live":
		return c.livePOPURL, nil
	default:
		return nil, errors.New("Duitku environment must be sandbox or live")
	}
}

func (c *Client) apiBaseURL(environment string) (*url.URL, error) {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "sandbox":
		return c.sandboxAPIURL, nil
	case "live":
		return c.liveAPIURL, nil
	default:
		return nil, errors.New("Duitku environment must be sandbox or live")
	}
}

func parseCredentials(input map[string]string) (apiCredentials, error) {
	credentials := apiCredentials{
		merchantCode: strings.TrimSpace(input["merchant_code"]),
		apiKey:       strings.TrimSpace(input["api_key"]),
	}
	if credentials.merchantCode == "" || len(credentials.merchantCode) > 128 || strings.ContainsAny(credentials.merchantCode, "\r\n") {
		return apiCredentials{}, errors.New("Duitku merchant_code is required and must be valid")
	}
	if credentials.apiKey == "" || len(credentials.apiKey) > 512 || strings.ContainsAny(credentials.apiKey, "\r\n") {
		return apiCredentials{}, errors.New("Duitku api_key is required and must be valid")
	}
	return credentials, nil
}

func sign(key, value string) string {
	digest := hmac.New(sha256.New, []byte(key))
	_, _ = digest.Write([]byte(value))
	return hex.EncodeToString(digest.Sum(nil))
}

func providerAmount(amount int64, currency string) (int64, error) {
	if !strings.EqualFold(strings.TrimSpace(currency), "IDR") {
		return 0, errors.New("Duitku POP connector supports IDR only")
	}
	if amount < 10_000 {
		return 0, errors.New("Duitku payment amount must be at least IDR 10,000")
	}
	return amount, nil
}

func merchantOrderID(input connector.PaymentInput) (string, error) {
	value := strings.TrimSpace(input.LocalPaymentID)
	if value == "" {
		value = strings.TrimSpace(input.MerchantReference)
	}
	value = strings.Map(func(char rune) rune {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			return char
		}
		return '-'
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "", errors.New("local_payment_id or merchant_reference is required for Duitku")
	}
	if len(value) > 50 {
		value = value[:50]
	}
	return value, nil
}

func customerEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	parsed, err := mail.ParseAddress(value)
	if err != nil || !strings.EqualFold(parsed.Address, value) {
		return "", errors.New("customer.email must be a valid email address for Duitku checkout")
	}
	return value, nil
}

func customerName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Emisell Customer"
	}
	characters := []rune(value)
	if len(characters) > 20 {
		value = string(characters[:20])
	}
	return value
}

func itemDetails(items []connector.Item, currency string, expected int64) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	var total int64
	for _, item := range items {
		price, err := providerAmountForItem(item.NetUnitAmount, currency)
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
		result = append(result, map[string]any{"name": name, "price": price, "quantity": quantity})
		total += price * int64(quantity)
	}
	if len(result) == 0 || total != expected {
		return nil
	}
	return result
}

func providerAmountForItem(amount int64, currency string) (int64, error) {
	if !strings.EqualFold(strings.TrimSpace(currency), "IDR") || amount <= 0 {
		return 0, errors.New("invalid Duitku item amount")
	}
	return amount, nil
}

func expiryPeriod(value string, now time.Time) (int64, error) {
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
	if minutes > 1440 {
		minutes = 1440
	}
	return minutes, nil
}

func paymentStatus(code, message string) string {
	switch strings.TrimSpace(code) {
	case "00":
		return "SUCCEEDED"
	case "01", "":
		return "PENDING"
	case "02":
		lower := strings.ToLower(message)
		switch {
		case strings.Contains(lower, "expir"):
			return "EXPIRED"
		case strings.Contains(lower, "cancel"):
			return "CANCELLED"
		default:
			return "FAILED"
		}
	default:
		return "FAILED"
	}
}

func duitkuAPIError(status int, response map[string]any) *connector.APIError {
	code := firstString(response, "statusCode", "responseCode")
	if code == "" {
		code = "DUITKU_ERROR"
	}
	message := firstString(response, "statusMessage", "responseMessage", "message")
	if message == "" {
		message = "Duitku rejected the request"
	}
	return &connector.APIError{Provider: "duitku", Status: status, Code: code, Message: message}
}

func invalidWebhookSignature() *connector.APIError {
	return &connector.APIError{Provider: "duitku", Status: http.StatusUnauthorized, Code: "INVALID_WEBHOOK_SIGNATURE"}
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
