package midtrans

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
	"qris":              {providerMethod: "real_time_payment", providerType: "qris", profile: "midtrans-core-v2/qris"},
	"va_bca":            {providerMethod: "bank_transfer", providerType: "bca", profile: "midtrans-core-v2/va_bca"},
	"va_mandiri":        {providerMethod: "bank_transfer", providerType: "mandiri", profile: "midtrans-core-v2/va_mandiri"},
	"va_bni":            {providerMethod: "bank_transfer", providerType: "bni", profile: "midtrans-core-v2/va_bni"},
	"va_bri":            {providerMethod: "bank_transfer", providerType: "bri", profile: "midtrans-core-v2/va_bri"},
	"va_permata":        {providerMethod: "bank_transfer", providerType: "permata", profile: "midtrans-core-v2/va_permata"},
	"va_cimb":           {providerMethod: "bank_transfer", providerType: "cimb", profile: "midtrans-core-v2/va_cimb"},
	"ewallet_gopay":     {providerMethod: "wallet", providerType: "gopay", profile: "midtrans-core-v2/ewallet_gopay"},
	"ewallet_shopeepay": {providerMethod: "wallet", providerType: "shopeepay", profile: "midtrans-core-v2/ewallet_shopeepay"},
}

func (c *Client) Manifest() connector.Manifest {
	profiles := make(map[string]connector.CertificationProfile, len(supportedMethods))
	for method, mapping := range supportedMethods {
		profiles[method] = connector.CertificationProfile{
			Code:             mapping.profile,
			Automated:        true,
			WebhookSetupHint: "Set the Midtrans Payment Notification URL to the Payment Proxy provider webhook URL, then complete the sandbox action and resume certification. A documented method remains unavailable to checkout until this certification passes.",
		}
	}
	return connector.Manifest{
		Code:             c.Code(),
		Name:             "Midtrans",
		Version:          "emisell-midtrans-v1.1.0",
		Runtime:          "isolated_container",
		ExecutableSHA256: c.executableSHA256,
		Operations: []connector.Operation{
			connector.OperationVerifyInstallation,
			connector.OperationDisableInstallation,
			connector.OperationCreatePayment,
			connector.OperationGetPayment,
			connector.OperationHandleWebhook,
		},
		CredentialFields: []connector.CredentialField{
			{Code: "server_key", Label: "Server key", InputType: "password", Secret: true, Required: true},
			{Code: "pop_id", Label: "PoP ID (Core API)", InputType: "password", Secret: true, Required: false},
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
	method := strings.ToLower(strings.TrimSpace(input.ProviderMethod))
	methodType := strings.ToLower(strings.TrimSpace(input.ProviderMethodType))
	methodMatches := method == mapping.providerMethod || (code == "qris" && method == "qr_code")
	if !methodMatches || methodType != mapping.providerType {
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
		return errors.New("Midtrans Core API connector currently supports IDR only")
	}
	if input.Amount%100 != 0 {
		return errors.New("Midtrans IDR amount must use whole rupiah expressed in minor units")
	}
	return nil
}
