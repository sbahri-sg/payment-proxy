package remoteconnector

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/partnercontract"
)

const maxResponseBytes = 1 << 20

type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
	manifest   connector.Manifest
}

func Discover(ctx context.Context, baseURL, token string, timeout time.Duration) ([]connector.Connector, error) {
	return DiscoverWithCAPEM(ctx, baseURL, token, timeout, nil)
}

func DiscoverWithCAPEM(ctx context.Context, baseURL, token string, timeout time.Duration, caPEM []byte) ([]connector.Connector, error) {
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("connector runner token is required")
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if len(caPEM) > 0 {
		roots, rootsErr := x509.SystemCertPool()
		if rootsErr != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("connector runner CA contains no valid certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("connector runner redirect is not allowed")
		},
	}
	bootstrap := &Client{baseURL: parsed, token: strings.TrimSpace(token), httpClient: httpClient}
	var response partnercontract.CapabilitiesResponse
	if err := bootstrap.do(ctx, http.MethodGet, "/partner/v1/capabilities", nil, &response, false); err != nil {
		return nil, fmt.Errorf("discover connector runner: %w", err)
	}
	if response.Data.ContractVersion != partnercontract.Version {
		return nil, fmt.Errorf("connector runner contract %q is not supported", response.Data.ContractVersion)
	}
	if len(response.Data.Connectors) == 0 {
		return nil, errors.New("connector runner returned no connectors")
	}
	result := make([]connector.Connector, 0, len(response.Data.Connectors))
	seen := map[string]bool{}
	for _, manifest := range response.Data.Connectors {
		if err := manifest.Validate(); err != nil {
			return nil, fmt.Errorf("connector runner manifest: %w", err)
		}
		runtimeID := manifest.Code + "@" + manifest.Version
		if seen[runtimeID] {
			return nil, fmt.Errorf("connector runner returned duplicate connector runtime %q", runtimeID)
		}
		seen[runtimeID] = true
		result = append(result, &Client{baseURL: parsed, token: bootstrap.token, httpClient: httpClient, manifest: manifest.Clone()})
	}
	return result, nil
}

func parseBaseURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, errors.New("invalid connector runner base URL")
	}
	return parsed, nil
}

func (c *Client) Code() string { return c.manifest.Code }

func (c *Client) Manifest() connector.Manifest { return c.manifest.Clone() }

func (c *Client) Ping(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("/health/ready"), nil)
	if err != nil {
		return err
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseBytes))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("connector runner readiness returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (c *Client) ValidatePaymentMethod(input connector.PaymentMethodMapping) error {
	payload := struct {
		ProviderCode    string                         `json:"provider_code"`
		ProviderVersion string                         `json:"provider_version"`
		Input           connector.PaymentMethodMapping `json:"input"`
	}{ProviderCode: c.Code(), ProviderVersion: c.manifest.Version, Input: input}
	return c.do(context.Background(), http.MethodPost, "/partner/v1/payment-methods/validate", payload, nil, false)
}

func (c *Client) ValidateHostedPaymentMethods(input []connector.PaymentMethodMapping) error {
	payload := struct {
		ProviderCode    string                           `json:"provider_code"`
		ProviderVersion string                           `json:"provider_version"`
		Input           []connector.PaymentMethodMapping `json:"input"`
	}{ProviderCode: c.Code(), ProviderVersion: c.manifest.Version, Input: input}
	return c.do(context.Background(), http.MethodPost, "/partner/v1/hosted-payment-methods/validate", payload, nil, false)
}

func (c *Client) ValidatePayment(input connector.PaymentValidation) error {
	payload := struct {
		ProviderCode    string                      `json:"provider_code"`
		ProviderVersion string                      `json:"provider_version"`
		Input           connector.PaymentValidation `json:"input"`
	}{ProviderCode: c.Code(), ProviderVersion: c.manifest.Version, Input: input}
	return c.do(context.Background(), http.MethodPost, "/partner/v1/payments/validate", payload, nil, false)
}

func (c *Client) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	var response partnercontract.DataResponse[connector.InstallationResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/installations/verify", input, &response, false)
	if err == nil {
		response.Data.StoredCredentials = make(map[string]string, len(input.Credentials))
		for key, value := range input.Credentials {
			response.Data.StoredCredentials[key] = value
		}
	}
	return response.Data, err
}

func (c *Client) DisableInstallation(ctx context.Context, input connector.InstallationInput) error {
	return c.do(ctx, http.MethodPost, "/partner/v1/installations/disable", input, nil, false)
}

func (c *Client) CreatePayment(ctx context.Context, input connector.PaymentInput) (connector.PaymentResult, error) {
	var response partnercontract.DataResponse[connector.PaymentResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/payments/create", input, &response, true)
	return response.Data, err
}

func (c *Client) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	var response partnercontract.DataResponse[connector.PaymentResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/payments/get", input, &response, false)
	return response.Data, err
}

func (c *Client) CapturePayment(ctx context.Context, input connector.CaptureInput) (connector.PaymentResult, error) {
	var response partnercontract.DataResponse[connector.PaymentResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/payments/capture", input, &response, true)
	return response.Data, err
}

func (c *Client) CancelPayment(ctx context.Context, input connector.PaymentLookup, key, reason string) (connector.PaymentResult, error) {
	var response partnercontract.DataResponse[connector.PaymentResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/payments/cancel", partnercontract.CancelPaymentRequest{
		Input: input, IdempotencyKey: key, Reason: reason,
	}, &response, true)
	return response.Data, err
}

func (c *Client) SimulatePayment(ctx context.Context, input connector.PaymentLookup, amount int64, currency string) error {
	return c.do(ctx, http.MethodPost, "/partner/v1/payments/simulate", partnercontract.SimulatePaymentRequest{
		Input: input, Amount: amount, Currency: currency,
	}, nil, true)
}

func (c *Client) CreateRefund(ctx context.Context, input connector.RefundInput) (connector.RefundResult, error) {
	var response partnercontract.DataResponse[connector.RefundResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/refunds/create", input, &response, true)
	return response.Data, err
}

func (c *Client) GetRefund(ctx context.Context, input connector.RefundLookup) (connector.RefundResult, error) {
	var response partnercontract.DataResponse[connector.RefundResult]
	err := c.do(ctx, http.MethodPost, "/partner/v1/refunds/get", input, &response, false)
	return response.Data, err
}

func (c *Client) HandleWebhook(ctx context.Context, input connector.WebhookInput) (connector.WebhookEvent, error) {
	var response partnercontract.DataResponse[connector.WebhookEvent]
	err := c.do(ctx, http.MethodPost, "/partner/v1/webhooks/normalize", input, &response, false)
	return response.Data, err
}

func (c *Client) resolve(path string) string {
	reference := &url.URL{Path: path}
	return c.baseURL.ResolveReference(reference).String()
}

func (c *Client) do(ctx context.Context, method, path string, input, output any, mutation bool) error {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.resolve(path), body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Accept", "application/json")
	if input != nil {
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
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	if len(payload) > maxResponseBytes {
		if mutation {
			return &connector.UnknownOutcomeError{Cause: errors.New("connector runner response exceeds limit")}
		}
		return errors.New("connector runner response exceeds limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem partnercontract.ErrorResponse
		if err := json.Unmarshal(payload, &problem); err != nil || problem.Error.Code == "" {
			return &connector.APIError{Provider: c.Code(), Status: response.StatusCode, Code: "CONNECTOR_RUNNER_ERROR", Message: "connector runner returned an invalid error response"}
		}
		switch problem.Error.Code {
		case "CONNECTOR_OUTCOME_UNKNOWN":
			return &connector.UnknownOutcomeError{Cause: errors.New("connector runner reported unknown outcome")}
		case "OPERATION_NOT_SUPPORTED":
			return connector.ErrNotSupported
		case "HOSTED_PAYMENT_METHOD_RESTRICTION_UNSUPPORTED":
			return fmt.Errorf("%w: %s", connector.ErrHostedPaymentRestrictionUnsupported, problem.Error.Message)
		}
		provider := problem.Error.Provider
		if provider == "" {
			provider = c.Code()
		}
		status := problem.Error.Status
		if status == 0 {
			status = response.StatusCode
		}
		return &connector.APIError{Provider: provider, Status: status, Code: problem.Error.Code, Message: problem.Error.Message}
	}
	if output == nil || len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, output); err != nil {
		if mutation {
			return &connector.UnknownOutcomeError{Cause: err}
		}
		return err
	}
	return nil
}
