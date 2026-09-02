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
	channelCode    string
	profile        string
	direct         bool
}

var supportedMethods = map[string]methodMapping{
	"qris":              {providerMethod: "real_time_payment", providerType: "qris", channelCode: "other_qris", profile: "midtrans-core-v2/qris", direct: true},
	"card":              {providerMethod: "card", providerType: "card", channelCode: "credit_card", profile: "midtrans-snap-v1/card"},
	"va_bca":            {providerMethod: "bank_transfer", providerType: "bca", channelCode: "bca_va", profile: "midtrans-core-v2/va_bca", direct: true},
	"va_mandiri":        {providerMethod: "bank_transfer", providerType: "mandiri", channelCode: "echannel", profile: "midtrans-core-v2/va_mandiri", direct: true},
	"va_bni":            {providerMethod: "bank_transfer", providerType: "bni", channelCode: "bni_va", profile: "midtrans-core-v2/va_bni", direct: true},
	"va_bri":            {providerMethod: "bank_transfer", providerType: "bri", channelCode: "bri_va", profile: "midtrans-core-v2/va_bri", direct: true},
	"va_permata":        {providerMethod: "bank_transfer", providerType: "permata", channelCode: "permata_va", profile: "midtrans-core-v2/va_permata", direct: true},
	"va_cimb":           {providerMethod: "bank_transfer", providerType: "cimb", channelCode: "cimb_va", profile: "midtrans-core-v2/va_cimb", direct: true},
	"va_danamon":        {providerMethod: "bank_transfer", providerType: "danamon", channelCode: "danamon_va", profile: "midtrans-snap-v1/va_danamon"},
	"va_bsi":            {providerMethod: "bank_transfer", providerType: "bsi", channelCode: "bsi_va", profile: "midtrans-snap-v1/va_bsi"},
	"ewallet_gopay":     {providerMethod: "wallet", providerType: "gopay", channelCode: "gopay", profile: "midtrans-core-v2/ewallet_gopay", direct: true},
	"ewallet_ovo":       {providerMethod: "wallet", providerType: "ovo", channelCode: "ovo", profile: "midtrans-snap-v1/ewallet_ovo"},
	"ewallet_dana":      {providerMethod: "wallet", providerType: "dana", channelCode: "dana", profile: "midtrans-snap-v1/ewallet_dana"},
	"ewallet_shopeepay": {providerMethod: "wallet", providerType: "shopeepay", channelCode: "shopeepay", profile: "midtrans-core-v2/ewallet_shopeepay", direct: true},
	"retail_alfamart":   {providerMethod: "retail", providerType: "alfamart", channelCode: "alfamart", profile: "midtrans-snap-v1/retail_alfamart"},
	"retail_indomaret":  {providerMethod: "retail", providerType: "indomaret", channelCode: "indomaret", profile: "midtrans-snap-v1/retail_indomaret"},
	"paylater_akulaku":  {providerMethod: "paylater", providerType: "akulaku", channelCode: "akulaku", profile: "midtrans-snap-v1/paylater_akulaku"},
	"paylater_kredivo":  {providerMethod: "paylater", providerType: "kredivo", channelCode: "kredivo", profile: "midtrans-snap-v1/paylater_kredivo"},
}

func (c *Client) Manifest() connector.Manifest {
	profiles := make(map[string]connector.CertificationProfile, len(supportedMethods))
	for method, mapping := range supportedMethods {
		checkoutMode := connector.CheckoutModeProviderHosted
		if mapping.direct {
			checkoutMode = connector.CheckoutModeDirect
		}
		profiles[method] = connector.CertificationProfile{
			Code:             mapping.profile,
			Automated:        true,
			CheckoutMode:     checkoutMode,
			WebhookSetupHint: "Set the Midtrans Payment Notification URL to the Payment Proxy provider webhook URL, then complete the sandbox action and resume certification. A documented method can be assigned; certification adds verified sandbox evidence.",
		}
	}
	return connector.Manifest{
		Code:             c.Code(),
		Name:             "Midtrans",
		Version:          "emisell-midtrans-v2.0.3",
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
	if channelCode := strings.ToLower(strings.TrimSpace(input.ProviderChannelCode)); channelCode != "" && channelCode != mapping.channelCode {
		return fmt.Errorf("payment method %q has an invalid %s channel mapping", code, c.Code())
	}
	return nil
}

func (c *Client) ValidateHostedPaymentMethods(methods []connector.PaymentMethodMapping) error {
	_, err := c.hostedPaymentTypes(methods)
	return err
}

func (c *Client) ValidatePayment(input connector.PaymentValidation) error {
	code := strings.ToLower(strings.TrimSpace(input.PaymentMethodCode))
	mapping, supported := supportedMethods[code]
	if !supported {
		return fmt.Errorf("payment method %q is not supported by connector %s", code, c.Code())
	}
	if input.CheckoutMode != connector.CheckoutModeProviderHosted && !mapping.direct {
		return fmt.Errorf("payment method %q is supported by Midtrans Snap hosted checkout only", code)
	}
	if input.Amount <= 0 {
		return errors.New("payment amount must be positive")
	}
	if !strings.EqualFold(strings.TrimSpace(input.Currency), "IDR") {
		return errors.New("Midtrans Core API connector currently supports IDR only")
	}
	return nil
}
