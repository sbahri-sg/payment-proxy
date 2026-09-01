package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

var (
	ErrOutcomeUnknown                      = errors.New("connector outcome is unknown")
	ErrNotSupported                        = errors.New("connector operation is not supported")
	ErrInvalidCredential                   = errors.New("provider credential is invalid")
	ErrHostedPaymentRestrictionUnsupported = errors.New("connector cannot safely restrict hosted checkout payment methods")
)

const (
	CheckoutModeDirect         = "direct"
	CheckoutModeProviderHosted = "provider_hosted"

	PaymentMethodAvailabilityAvailable   = "AVAILABLE"
	PaymentMethodAvailabilityUnavailable = "UNAVAILABLE"
)

type APIError struct {
	Provider string
	Status   int
	Code     string
	Message  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("%s returned HTTP %d (%s)", e.Provider, e.Status, e.Code)
}

type UnknownOutcomeError struct{ Cause error }

func (e *UnknownOutcomeError) Error() string { return ErrOutcomeUnknown.Error() }
func (e *UnknownOutcomeError) Unwrap() error { return e.Cause }
func (e *UnknownOutcomeError) Is(target error) bool {
	return target == ErrOutcomeUnknown
}

type InstallationInput struct {
	InstallationID   string            `json:"installation_id"`
	ProviderCode     string            `json:"provider_code"`
	ProviderVersion  string            `json:"provider_version,omitempty"`
	Environment      string            `json:"environment"`
	Credentials      map[string]string `json:"credentials"`
	PublicWebhookURL string            `json:"public_webhook_url,omitempty"`
}

type InstallationResult struct {
	ConnectorID               string                      `json:"connector_id"`
	Environment               string                      `json:"environment"`
	StoredCredentials         map[string]string           `json:"stored_credentials,omitempty"`
	WebhookReady              bool                        `json:"webhook_ready"`
	PaymentMethodAvailability []PaymentMethodAvailability `json:"payment_method_availability,omitempty"`
}

// PaymentMethodAvailability is optional runtime evidence returned by a
// connector during installation verification. It is deliberately separate
// from the merchant's ACTIVE/INACTIVE assignment: an outage must hide a method
// from checkout without silently changing the merchant's configuration.
type PaymentMethodAvailability struct {
	PaymentMethodCode string `json:"payment_method_code"`
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
}

type Customer struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type Item struct {
	ReferenceID   string `json:"reference_id"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	NetUnitAmount int64  `json:"net_unit_amount"`
	Quantity      int    `json:"quantity"`
	Category      string `json:"category"`
}

type PaymentInput struct {
	ProviderCode          string                 `json:"provider_code"`
	ProviderVersion       string                 `json:"provider_version,omitempty"`
	Environment           string                 `json:"environment"`
	Credentials           map[string]string      `json:"credentials"`
	InstallationID        string                 `json:"installation_id"`
	LocalPaymentID        string                 `json:"local_payment_id"`
	MerchantReference     string                 `json:"merchant_reference"`
	IdempotencyKey        string                 `json:"idempotency_key"`
	Amount                int64                  `json:"amount"`
	Currency              string                 `json:"currency"`
	CheckoutMode          string                 `json:"checkout_mode,omitempty"`
	PaymentMethodCode     string                 `json:"payment_method_code"`
	ChannelCode           string                 `json:"channel_code,omitempty"`
	AllowedPaymentMethods []PaymentMethodMapping `json:"allowed_payment_methods,omitempty"`
	PublicWebhookURL      string                 `json:"public_webhook_url,omitempty"`
	Customer              Customer               `json:"customer"`
	Items                 []Item                 `json:"items,omitempty"`
	ReturnURL             string                 `json:"return_url,omitempty"`
	Description           string                 `json:"description,omitempty"`
	ExpiresAt             string                 `json:"expires_at,omitempty"`
	Metadata              map[string]any         `json:"metadata,omitempty"`
}

type PaymentLookup struct {
	ProviderCode    string            `json:"provider_code"`
	ProviderVersion string            `json:"provider_version,omitempty"`
	Environment     string            `json:"environment"`
	Credentials     map[string]string `json:"credentials"`
	PaymentID       string            `json:"payment_id"`
}

type CaptureInput struct {
	ProviderCode    string            `json:"provider_code"`
	ProviderVersion string            `json:"provider_version,omitempty"`
	Environment     string            `json:"environment"`
	Credentials     map[string]string `json:"credentials"`
	PaymentID       string            `json:"payment_id"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Amount          int64             `json:"amount"`
	Currency        string            `json:"currency"`
}

type PaymentResult struct {
	ID                     string          `json:"id"`
	Status                 string          `json:"status"`
	ConnectorTransactionID string          `json:"connector_transaction_id,omitempty"`
	ClientSecret           string          `json:"client_secret,omitempty"`
	NextAction             json.RawMessage `json:"next_action,omitempty"`
}

type RefundInput struct {
	ProviderCode    string            `json:"provider_code"`
	ProviderVersion string            `json:"provider_version,omitempty"`
	Environment     string            `json:"environment"`
	Credentials     map[string]string `json:"credentials"`
	PaymentID       string            `json:"payment_id"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Amount          int64             `json:"amount"`
	Currency        string            `json:"currency"`
	Reason          string            `json:"reason,omitempty"`
	Metadata        map[string]any    `json:"metadata,omitempty"`
}

type RefundLookup struct {
	ProviderCode    string            `json:"provider_code"`
	ProviderVersion string            `json:"provider_version,omitempty"`
	Environment     string            `json:"environment"`
	Credentials     map[string]string `json:"credentials"`
	RefundID        string            `json:"refund_id"`
}

type RefundResult struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type WebhookInput struct {
	ProviderCode    string            `json:"provider_code"`
	ProviderVersion string            `json:"provider_version,omitempty"`
	Credentials     map[string]string `json:"credentials"`
	Headers         http.Header       `json:"headers"`
	Body            []byte            `json:"body"`
}

type WebhookEvent struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	PaymentID string `json:"payment_id,omitempty"`
	RefundID  string `json:"refund_id,omitempty"`
	Status    string `json:"status"`
}

type Connector interface {
	Code() string
	Manifest() Manifest
	ValidatePaymentMethod(PaymentMethodMapping) error
	ValidateHostedPaymentMethods([]PaymentMethodMapping) error
	ValidatePayment(PaymentValidation) error
	VerifyInstallation(context.Context, InstallationInput) (InstallationResult, error)
	DisableInstallation(context.Context, InstallationInput) error
	CreatePayment(context.Context, PaymentInput) (PaymentResult, error)
	GetPayment(context.Context, PaymentLookup) (PaymentResult, error)
	CapturePayment(context.Context, CaptureInput) (PaymentResult, error)
	CancelPayment(context.Context, PaymentLookup, string, string) (PaymentResult, error)
	SimulatePayment(context.Context, PaymentLookup, int64, string) error
	CreateRefund(context.Context, RefundInput) (RefundResult, error)
	GetRefund(context.Context, RefundLookup) (RefundResult, error)
	HandleWebhook(context.Context, WebhookInput) (WebhookEvent, error)
}
