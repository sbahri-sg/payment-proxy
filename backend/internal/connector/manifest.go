package connector

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Operation is a provider capability understood by the Payment Kernel.
type Operation string

const (
	OperationVerifyInstallation   Operation = "verify_installation"
	OperationDisableInstallation  Operation = "disable_installation"
	OperationCreatePayment        Operation = "create_payment"
	OperationCreateHostedCheckout Operation = "create_hosted_checkout"
	OperationGetPayment           Operation = "get_payment"
	OperationCapturePayment       Operation = "capture_payment"
	OperationCancelPayment        Operation = "cancel_payment"
	OperationSimulatePayment      Operation = "simulate_payment"
	OperationCreateRefund         Operation = "create_refund"
	OperationGetRefund            Operation = "get_refund"
	OperationHandleWebhook        Operation = "handle_webhook"
)

var validOperations = map[Operation]struct{}{
	OperationVerifyInstallation:   {},
	OperationDisableInstallation:  {},
	OperationCreatePayment:        {},
	OperationCreateHostedCheckout: {},
	OperationGetPayment:           {},
	OperationCapturePayment:       {},
	OperationCancelPayment:        {},
	OperationSimulatePayment:      {},
	OperationCreateRefund:         {},
	OperationGetRefund:            {},
	OperationHandleWebhook:        {},
}

type CredentialField struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	InputType string `json:"input_type"`
	Secret    bool   `json:"secret"`
	Required  bool   `json:"required"`
}

// CertificationProfile describes the sandbox conformance flow for one
// canonical payment method. Provider-specific API details stay in the
// connector and are exposed to the Kernel only as an opaque profile code.
type CertificationProfile struct {
	Code             string `json:"code"`
	Automated        bool   `json:"automated"`
	WebhookSetupHint string `json:"webhook_setup_hint,omitempty"`
}

// Manifest is the immutable capability contract registered at process start.
// Database catalog records still decide which methods are visible at checkout;
// the manifest says what the running connector implementation can execute.
type Manifest struct {
	Code                  string                          `json:"code"`
	Name                  string                          `json:"name"`
	Version               string                          `json:"version"`
	Runtime               string                          `json:"runtime"`
	ExecutableSHA256      string                          `json:"executable_sha256,omitempty"`
	Operations            []Operation                     `json:"operations"`
	CredentialFields      []CredentialField               `json:"credential_fields"`
	CertificationProfiles map[string]CertificationProfile `json:"certification_profiles"`
}

func (m Manifest) Validate() error {
	if !validIdentifier(m.Code) || m.Code != strings.ToLower(strings.TrimSpace(m.Code)) {
		return errors.New("connector manifest code must be a lowercase identifier")
	}
	if strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Version) == "" || strings.TrimSpace(m.Runtime) == "" {
		return errors.New("connector manifest name, version, and runtime are required")
	}
	if m.ExecutableSHA256 != "" {
		if len(m.ExecutableSHA256) != 64 || strings.ToLower(m.ExecutableSHA256) != m.ExecutableSHA256 {
			return errors.New("connector executable_sha256 must be a lowercase SHA-256 digest")
		}
		if _, err := hex.DecodeString(m.ExecutableSHA256); err != nil {
			return errors.New("connector executable_sha256 must be a lowercase SHA-256 digest")
		}
	}
	if len(m.Operations) == 0 {
		return errors.New("connector manifest must declare at least one operation")
	}
	seenOperations := make(map[Operation]struct{}, len(m.Operations))
	for _, operation := range m.Operations {
		if _, valid := validOperations[operation]; !valid {
			return fmt.Errorf("connector manifest contains unknown operation %q", operation)
		}
		if _, duplicate := seenOperations[operation]; duplicate {
			return fmt.Errorf("connector manifest contains duplicate operation %q", operation)
		}
		seenOperations[operation] = struct{}{}
	}
	seenCredentials := make(map[string]struct{}, len(m.CredentialFields))
	for _, field := range m.CredentialFields {
		if !validIdentifier(field.Code) || strings.TrimSpace(field.Label) == "" || strings.TrimSpace(field.InputType) == "" {
			return fmt.Errorf("connector manifest contains an invalid credential field %q", field.Code)
		}
		if _, duplicate := seenCredentials[field.Code]; duplicate {
			return fmt.Errorf("connector manifest contains duplicate credential field %q", field.Code)
		}
		seenCredentials[field.Code] = struct{}{}
	}
	for method, profile := range m.CertificationProfiles {
		if !validIdentifier(method) || strings.TrimSpace(profile.Code) == "" {
			return fmt.Errorf("connector manifest contains an invalid certification profile for %q", method)
		}
	}
	return nil
}

func (m Manifest) Supports(operation Operation) bool {
	for _, candidate := range m.Operations {
		if candidate == operation {
			return true
		}
	}
	return false
}

func (m Manifest) CertificationProfile(paymentMethodCode string) (CertificationProfile, bool) {
	profile, ok := m.CertificationProfiles[strings.ToLower(strings.TrimSpace(paymentMethodCode))]
	return profile, ok
}

func (m Manifest) Clone() Manifest {
	clone := m
	clone.Operations = append([]Operation(nil), m.Operations...)
	clone.CredentialFields = append([]CredentialField(nil), m.CredentialFields...)
	clone.CertificationProfiles = make(map[string]CertificationProfile, len(m.CertificationProfiles))
	for code, profile := range m.CertificationProfiles {
		clone.CertificationProfiles[code] = profile
	}
	return clone
}

type PaymentMethodMapping struct {
	PaymentMethodCode   string `json:"payment_method_code"`
	ProviderMethod      string `json:"provider_method"`
	ProviderMethodType  string `json:"provider_method_type"`
	ProviderChannelCode string `json:"provider_channel_code,omitempty"`
}

type PaymentValidation struct {
	PaymentMethodCode string `json:"payment_method_code"`
	Currency          string `json:"currency"`
	Amount            int64  `json:"amount"`
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || (char == '_' && index > 0) || (char == '-' && index > 0) {
			continue
		}
		return false
	}
	return true
}
