package doku

import (
	"errors"
	"fmt"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
)

type methodMapping struct {
	providerMethod string
	providerType   string
	channelCode    string
}

var supportedMethods = map[string]methodMapping{
	"qris":              {providerMethod: "real_time_payment", providerType: "qris", channelCode: "QRIS"},
	"card":              {providerMethod: "card", providerType: "card", channelCode: "CREDIT_CARD"},
	"va_bca":            {providerMethod: "bank_transfer", providerType: "bca", channelCode: "VIRTUAL_ACCOUNT_BCA"},
	"va_mandiri":        {providerMethod: "bank_transfer", providerType: "mandiri", channelCode: "VIRTUAL_ACCOUNT_BANK_MANDIRI"},
	"va_bni":            {providerMethod: "bank_transfer", providerType: "bni", channelCode: "VIRTUAL_ACCOUNT_BNI"},
	"va_bri":            {providerMethod: "bank_transfer", providerType: "bri", channelCode: "VIRTUAL_ACCOUNT_BRI"},
	"va_permata":        {providerMethod: "bank_transfer", providerType: "permata", channelCode: "VIRTUAL_ACCOUNT_BANK_PERMATA"},
	"va_cimb":           {providerMethod: "bank_transfer", providerType: "cimb", channelCode: "VIRTUAL_ACCOUNT_BANK_CIMB"},
	"va_danamon":        {providerMethod: "bank_transfer", providerType: "danamon", channelCode: "VIRTUAL_ACCOUNT_BANK_DANAMON"},
	"va_bsi":            {providerMethod: "bank_transfer", providerType: "bsi", channelCode: "VIRTUAL_ACCOUNT_BANK_SYARIAH_MANDIRI"},
	"va_bnc":            {providerMethod: "bank_transfer", providerType: "bnc", channelCode: "VIRTUAL_ACCOUNT_BNC"},
	"va_btn":            {providerMethod: "bank_transfer", providerType: "btn", channelCode: "VIRTUAL_ACCOUNT_BTN"},
	"va_doku":           {providerMethod: "bank_transfer", providerType: "doku", channelCode: "VIRTUAL_ACCOUNT_DOKU"},
	"ewallet_ovo":       {providerMethod: "wallet", providerType: "ovo", channelCode: "EMONEY_OVO"},
	"ewallet_dana":      {providerMethod: "wallet", providerType: "dana", channelCode: "EMONEY_DANA"},
	"ewallet_shopeepay": {providerMethod: "wallet", providerType: "shopeepay", channelCode: "EMONEY_SHOPEE_PAY"},
	"ewallet_linkaja":   {providerMethod: "wallet", providerType: "linkaja", channelCode: "EMONEY_LINKAJA"},
	"ewallet_doku":      {providerMethod: "wallet", providerType: "doku", channelCode: "EMONEY_DOKU"},
	"retail_alfamart":   {providerMethod: "retail", providerType: "alfamart", channelCode: "ONLINE_TO_OFFLINE_ALFA"},
	"retail_indomaret":  {providerMethod: "retail", providerType: "indomaret", channelCode: "ONLINE_TO_OFFLINE_INDOMARET"},
}

func (c *Client) Manifest() connector.Manifest {
	profiles := make(map[string]connector.CertificationProfile, len(supportedMethods))
	for code := range supportedMethods {
		profiles[code] = connector.CertificationProfile{
			Code:      "doku-checkout-v1/" + code,
			Automated: true,
			WebhookSetupHint: "Configure the exact installation webhook URL in DOKU Back Office for the selected channel. " +
				"DOKU requires a public HTTPS notification URL and signs the callback with the installation Secret Key.",
		}
	}
	return connector.Manifest{
		Code:             c.Code(),
		Name:             "DOKU",
		Version:          "emisell-doku-v2.0.1",
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
			{Code: "client_id", Label: "Client ID", InputType: "text", Secret: false, Required: true},
			{Code: "secret_key", Label: "Secret key", InputType: "password", Secret: true, Required: true},
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
		return errors.New("DOKU Checkout connector supports IDR only")
	}
	if input.Amount > 999_999_999_999 {
		return errors.New("DOKU payment amount exceeds the 12-digit provider limit")
	}
	return nil
}

func dokuChannel(methodCode, override string) (string, error) {
	mapping, ok := supportedMethods[strings.ToLower(strings.TrimSpace(methodCode))]
	if !ok {
		return "", connector.ErrNotSupported
	}
	override = strings.ToUpper(strings.TrimSpace(override))
	if override != "" && override != mapping.channelCode {
		return "", fmt.Errorf("DOKU channel %q is not valid for payment method %q", override, methodCode)
	}
	return mapping.channelCode, nil
}
