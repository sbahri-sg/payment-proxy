package xendit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

type methodMapping struct {
	providerMethod string
	providerType   string
	profile        string
}

var supportedMethods = map[string]methodMapping{
	"qris":                   {providerMethod: "qr_code", providerType: "qris", profile: "xendit-payments-v3/qris"},
	"card":                   {providerMethod: "card", providerType: "card", profile: "xendit-payment-session/card"},
	"va_bca":                 {providerMethod: "bank_transfer", providerType: "bca", profile: "xendit-payments-v3/va_bca"},
	"va_mandiri":             {providerMethod: "bank_transfer", providerType: "mandiri", profile: "xendit-payments-v3/va_mandiri"},
	"va_bni":                 {providerMethod: "bank_transfer", providerType: "bni", profile: "xendit-payments-v3/va_bni"},
	"va_bri":                 {providerMethod: "bank_transfer", providerType: "bri", profile: "xendit-payments-v3/va_bri"},
	"va_permata":             {providerMethod: "bank_transfer", providerType: "permata", profile: "xendit-payments-v3/va_permata"},
	"va_cimb":                {providerMethod: "bank_transfer", providerType: "cimb", profile: "xendit-payments-v3/va_cimb"},
	"va_danamon":             {providerMethod: "bank_transfer", providerType: "danamon", profile: "xendit-payments-v3/va_danamon"},
	"va_bsi":                 {providerMethod: "bank_transfer", providerType: "bsi", profile: "xendit-payments-v3/va_bsi"},
	"va_muamalat":            {providerMethod: "bank_transfer", providerType: "muamalat", profile: "xendit-payments-v3/va_muamalat"},
	"ewallet_ovo":            {providerMethod: "wallet", providerType: "ovo", profile: "xendit-payments-v3/ewallet_ovo"},
	"ewallet_dana":           {providerMethod: "wallet", providerType: "dana", profile: "xendit-payments-v3/ewallet_dana"},
	"ewallet_shopeepay":      {providerMethod: "wallet", providerType: "shopeepay", profile: "xendit-payments-v3/ewallet_shopeepay"},
	"ewallet_linkaja":        {providerMethod: "wallet", providerType: "linkaja", profile: "xendit-payments-v3/ewallet_linkaja"},
	"ewallet_astrapay":       {providerMethod: "wallet", providerType: "astrapay", profile: "xendit-payments-v3/ewallet_astrapay"},
	"digital_banking_jenius": {providerMethod: "digital_banking", providerType: "jenius", profile: "xendit-payments-v3/digital_banking_jenius"},
	"paylater_kredivo":       {providerMethod: "paylater", providerType: "kredivo", profile: "xendit-payments-v3/paylater_kredivo"},
	"paylater_akulaku":       {providerMethod: "paylater", providerType: "akulaku", profile: "xendit-payments-v3/paylater_akulaku"},
	"paylater_indodana":      {providerMethod: "paylater", providerType: "indodana", profile: "xendit-payments-v3/paylater_indodana"},
}

func (c *Client) Manifest() connector.Manifest {
	profiles := make(map[string]connector.CertificationProfile, len(supportedMethods))
	for code, mapping := range supportedMethods {
		profiles[code] = connector.CertificationProfile{
			Code:             mapping.profile,
			Automated:        true,
			WebhookSetupHint: "Configure both Payments API v3 webhook topics in Xendit Dashboard, then run certification again.",
		}
	}
	return connector.Manifest{
		Code:    c.Code(),
		Name:    "Xendit",
		Version: "emisell-xendit-v1",
		Runtime: "isolated_container",
		Operations: []connector.Operation{
			connector.OperationVerifyInstallation,
			connector.OperationDisableInstallation,
			connector.OperationCreatePayment,
			connector.OperationGetPayment,
			connector.OperationSimulatePayment,
			connector.OperationHandleWebhook,
		},
		CredentialFields: []connector.CredentialField{
			{Code: "api_key", Label: "Secret API key", InputType: "password", Secret: true, Required: true},
			{Code: "webhook_verification_token", Label: "Webhook verification token", InputType: "password", Secret: true, Required: true},
		},
		CertificationProfiles: profiles,
	}
}

func (c *Client) ValidatePaymentMethod(input connector.PaymentMethodMapping) error {
	code := strings.ToLower(strings.TrimSpace(input.PaymentMethodCode))
	mapping, supported := supportedMethods[code]
	if !supported {
		return fmt.Errorf("payment method %q is not supported by connector %s", code, c.Code())
	}
	providerMethod := strings.ToLower(strings.TrimSpace(input.ProviderMethod))
	providerType := strings.ToLower(strings.TrimSpace(input.ProviderMethodType))
	// Older installations used real_time_payment for QRIS. Accept it while the
	// catalog migration converges all installations to qr_code.
	methodMatches := providerMethod == mapping.providerMethod || (code == "qris" && providerMethod == "real_time_payment")
	if !methodMatches || providerType != mapping.providerType {
		return fmt.Errorf("payment method %q has an invalid %s mapping", code, c.Code())
	}
	return nil
}

func (c *Client) ValidatePayment(input connector.PaymentValidation) error {
	code := strings.ToLower(strings.TrimSpace(input.PaymentMethodCode))
	if _, supported := supportedMethods[code]; !supported {
		return fmt.Errorf("payment method %q is not supported by connector %s", code, c.Code())
	}
	if input.Amount <= 0 {
		return errors.New("payment amount must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(input.Currency), "IDR") {
		return nil
	}
	if input.Amount%100 != 0 {
		return errors.New("Xendit IDR amount must use whole rupiah expressed in minor units")
	}
	if code == "qris" && (input.Amount < 100 || input.Amount > 1_000_000_000) {
		return errors.New("Xendit QRIS amount must be between Rp1 and Rp10,000,000")
	}
	if strings.HasPrefix(code, "va_") && (input.Amount < 1_000_000 || input.Amount > 5_000_000_000) {
		return errors.New("Xendit Virtual Account amount must be between Rp10,000 and Rp50,000,000")
	}
	if code == "card" && (input.Amount < 500_000 || input.Amount > 20_000_000_000) {
		return errors.New("Xendit card amount must be between Rp5,000 and Rp200,000,000")
	}
	return nil
}
