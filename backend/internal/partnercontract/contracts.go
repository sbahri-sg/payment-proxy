package partnercontract

import "github.com/emisell/api-payment-proxy/internal/connector"

const Version = "v1"

type Capabilities struct {
	ContractVersion string               `json:"contract_version"`
	Connectors      []connector.Manifest `json:"connectors"`
}

type CapabilitiesResponse struct {
	Data Capabilities `json:"data"`
}

type DataResponse[T any] struct {
	Data T `json:"data"`
}

type Error struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Provider string `json:"provider,omitempty"`
	Status   int    `json:"provider_status,omitempty"`
}

type ErrorResponse struct {
	Error Error `json:"error"`
}

type CancelPaymentRequest struct {
	Input          connector.PaymentLookup `json:"input"`
	IdempotencyKey string                  `json:"idempotency_key"`
	Reason         string                  `json:"reason,omitempty"`
}

type SimulatePaymentRequest struct {
	Input    connector.PaymentLookup `json:"input"`
	Amount   int64                   `json:"amount"`
	Currency string                  `json:"currency"`
}
