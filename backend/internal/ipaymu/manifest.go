package ipaymu

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

type methodMapping struct {
	providerMethod string
	providerType   string
	paymentMethod  string
	channelCode    string
}

var supportedMethods = map[string]methodMapping{
	"qris":              {providerMethod: "real_time_payment", providerType: "qris", paymentMethod: "qris", channelCode: "mpm"},
	"card":              {providerMethod: "card", providerType: "card", paymentMethod: "cc", channelCode: "cc"},
	"va_arta_graha":     {providerMethod: "bank_transfer", providerType: "arta_graha", paymentMethod: "va", channelCode: "bag"},
	"va_bca":            {providerMethod: "bank_transfer", providerType: "bca", paymentMethod: "va", channelCode: "bca"},
	"va_bni":            {providerMethod: "bank_transfer", providerType: "bni", paymentMethod: "va", channelCode: "bni"},
	"va_cimb":           {providerMethod: "bank_transfer", providerType: "cimb", paymentMethod: "va", channelCode: "cimb"},
	"va_mandiri":        {providerMethod: "bank_transfer", providerType: "mandiri", paymentMethod: "va", channelCode: "mandiri"},
	"va_muamalat":       {providerMethod: "bank_transfer", providerType: "muamalat", paymentMethod: "va", channelCode: "bmi"},
	"va_bri":            {providerMethod: "bank_transfer", providerType: "bri", paymentMethod: "va", channelCode: "bri"},
	"va_bsi":            {providerMethod: "bank_transfer", providerType: "bsi", paymentMethod: "va", channelCode: "bsi"},
	"va_permata":        {providerMethod: "bank_transfer", providerType: "permata", paymentMethod: "va", channelCode: "permata"},
	"va_danamon":        {providerMethod: "bank_transfer", providerType: "danamon", paymentMethod: "va", channelCode: "danamon"},
	"va_btn":            {providerMethod: "bank_transfer", providerType: "btn", paymentMethod: "va", channelCode: "btn"},
	"retail_alfamart":   {providerMethod: "retail", providerType: "alfamart", paymentMethod: "cstore", channelCode: "alfamart"},
	"retail_indomaret":  {providerMethod: "retail", providerType: "indomaret", paymentMethod: "cstore", channelCode: "indomaret"},
	"ewallet_dana":      {providerMethod: "wallet", providerType: "dana", paymentMethod: "ewallet", channelCode: "dana"},
	"ewallet_shopeepay": {providerMethod: "wallet", providerType: "shopeepay", paymentMethod: "ewallet", channelCode: "shopeepay"},
	"paylater_akulaku":  {providerMethod: "paylater", providerType: "akulaku", paymentMethod: "paylater", channelCode: "akulaku"},
}

func (c *Client) Manifest() connector.Manifest {
	profiles := make(map[string]connector.CertificationProfile, len(supportedMethods))
	for code := range supportedMethods {
		profiles[code] = connector.CertificationProfile{
			Code:      "ipaymu-v2/" + code,
			Automated: true,
			WebhookSetupHint: "iPaymu notifyUrl dikirim per transaksi oleh Payment Proxy. " +
				"Selesaikan pembayaran sandbox lalu lanjutkan verifikasi callback X-Signature.",
		}
	}
	return connector.Manifest{
		Code:             c.Code(),
		Name:             "iPaymu",
		Version:          "emisell-ipaymu-v2.0.1",
		Runtime:          "isolated_container",
		ExecutableSHA256: c.executableSHA256,
		Operations: []connector.Operation{
			connector.OperationVerifyInstallation,
			connector.OperationDisableInstallation,
			connector.OperationCreatePayment,
			connector.OperationCreateHostedCheckout,
			connector.OperationGetPayment,
			connector.OperationHandleWebhook,
		},
		CredentialFields: []connector.CredentialField{
			{Code: "va", Label: "VA number", InputType: "text", Secret: false, Required: true},
			{Code: "api_key", Label: "API key", InputType: "password", Secret: true, Required: true},
		},
		CertificationProfiles: profiles,
	}
}

func (c *Client) ValidatePaymentMethod(input connector.PaymentMethodMapping) error {
	code := strings.ToLower(strings.TrimSpace(input.PaymentMethodCode))
	mapping, ok := supportedMethods[code]
	if !ok {
		return fmt.Errorf("payment method %q is not supported by connector %s", code, c.Code())
	}
	if strings.ToLower(strings.TrimSpace(input.ProviderMethod)) != mapping.providerMethod ||
		strings.ToLower(strings.TrimSpace(input.ProviderMethodType)) != mapping.providerType {
		return fmt.Errorf("payment method %q has an invalid %s mapping", code, c.Code())
	}
	return nil
}

func (c *Client) ValidatePayment(input connector.PaymentValidation) error {
	if _, ok := supportedMethods[strings.ToLower(strings.TrimSpace(input.PaymentMethodCode))]; !ok {
		return fmt.Errorf("payment method %q is not supported by connector %s", input.PaymentMethodCode, c.Code())
	}
	if input.Amount <= 0 {
		return errors.New("payment amount must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(input.Currency), "IDR") {
		return errors.New("iPaymu connector supports IDR only")
	}
	return nil
}

func ipaymuChannel(methodCode, override string) (methodMapping, error) {
	mapping, ok := supportedMethods[strings.ToLower(strings.TrimSpace(methodCode))]
	if !ok {
		return methodMapping{}, connector.ErrNotSupported
	}
	override = strings.ToLower(strings.TrimSpace(override))
	if override != "" && override != mapping.channelCode {
		return methodMapping{}, fmt.Errorf("iPaymu channel %q is not valid for payment method %q", override, methodCode)
	}
	return mapping, nil
}
