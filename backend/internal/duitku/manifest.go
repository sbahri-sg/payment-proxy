package duitku

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
	alternatives   map[string]struct{}
}

func mapping(providerMethod, providerType, channelCode string, alternatives ...string) methodMapping {
	allowed := make(map[string]struct{}, len(alternatives)+1)
	allowed[channelCode] = struct{}{}
	for _, code := range alternatives {
		allowed[code] = struct{}{}
	}
	return methodMapping{providerMethod: providerMethod, providerType: providerType, channelCode: channelCode, alternatives: allowed}
}

var supportedMethods = map[string]methodMapping{
	"qris":                   mapping("real_time_payment", "qris", "SP", "NQ", "GQ", "SQ"),
	"card":                   mapping("card", "card", "VC"),
	"va_bca":                 mapping("bank_transfer", "bca", "BC"),
	"va_mandiri":             mapping("bank_transfer", "mandiri", "M2"),
	"va_bni":                 mapping("bank_transfer", "bni", "I1"),
	"va_bri":                 mapping("bank_transfer", "bri", "BR"),
	"va_permata":             mapping("bank_transfer", "permata", "BT"),
	"va_cimb":                mapping("bank_transfer", "cimb", "B1"),
	"va_danamon":             mapping("bank_transfer", "danamon", "DM"),
	"va_bsi":                 mapping("bank_transfer", "bsi", "BV"),
	"va_maybank":             mapping("bank_transfer", "maybank", "VA"),
	"va_bnc":                 mapping("bank_transfer", "bnc", "NC"),
	"va_atm_bersama":         mapping("bank_transfer", "atm_bersama", "A1"),
	"va_arta_graha":          mapping("bank_transfer", "arta_graha", "AG"),
	"va_sahabat_sampoerna":   mapping("bank_transfer", "sahabat_sampoerna", "S1"),
	"ewallet_ovo":            mapping("wallet", "ovo", "OV", "OL"),
	"ewallet_dana":           mapping("wallet", "dana", "DA"),
	"ewallet_shopeepay":      mapping("wallet", "shopeepay", "SA", "SL"),
	"ewallet_linkaja":        mapping("wallet", "linkaja", "LF", "LA"),
	"retail_alfamart":        mapping("retail", "alfamart", "FT"),
	"retail_indomaret":       mapping("retail", "indomaret", "IR"),
	"retail_pegadaian_pos":   mapping("retail", "pegadaian_pos", "FT"),
	"paylater_indodana":      mapping("paylater", "indodana", "DN"),
	"paylater_atome":         mapping("paylater", "atome", "AT"),
	"digital_banking_jenius": mapping("digital_banking", "jenius", "JP"),
}

func (c *Client) Manifest() connector.Manifest {
	profiles := make(map[string]connector.CertificationProfile, len(supportedMethods))
	for code := range supportedMethods {
		profiles[code] = connector.CertificationProfile{
			Code:      "duitku-pop-v2/" + code,
			Automated: true,
			WebhookSetupHint: "Duitku callbackUrl is supplied per transaction by Payment Proxy. " +
				"Complete the sandbox payment and resume certification to verify the callback.",
		}
	}
	return connector.Manifest{
		Code:             c.Code(),
		Name:             "Duitku",
		Version:          "emisell-duitku-v2.0.1",
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
			{Code: "merchant_code", Label: "Merchant code", InputType: "text", Secret: false, Required: true},
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
		return errors.New("Duitku POP connector supports IDR only")
	}
	if input.Amount < 10_000 {
		return errors.New("Duitku payment amount must be at least IDR 10,000")
	}
	return nil
}

func duitkuChannel(methodCode, override string) (string, error) {
	mapping, ok := supportedMethods[strings.ToLower(strings.TrimSpace(methodCode))]
	if !ok {
		return "", connector.ErrNotSupported
	}
	channel := strings.ToUpper(strings.TrimSpace(override))
	if channel == "" {
		return mapping.channelCode, nil
	}
	if _, ok = mapping.alternatives[channel]; !ok {
		return "", fmt.Errorf("Duitku channel %q is not valid for payment method %q", channel, methodCode)
	}
	return channel, nil
}
