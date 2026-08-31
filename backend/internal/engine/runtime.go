package engine

import (
	"context"
	"fmt"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/registry"
)

// Runtime is the Emisell-owned payment engine. It knows only the universal
// Connector contract; provider-specific behavior remains inside each adapter.
type Runtime struct {
	registry *registry.Registry
}

func New(connectorRegistry *registry.Registry) (*Runtime, error) {
	if connectorRegistry == nil {
		return nil, fmt.Errorf("connector registry is required")
	}
	return &Runtime{registry: connectorRegistry}, nil
}

func (r *Runtime) Ping(ctx context.Context) error {
	if r.registry == nil || len(r.registry.Codes()) == 0 {
		return fmt.Errorf("connector registry is empty")
	}
	return r.registry.Ping(ctx)
}

func (r *Runtime) ConnectorCodes() []string {
	return r.registry.Codes()
}

func (r *Runtime) connector(code string) (connector.Connector, error) {
	return r.registry.Connector(code)
}

func (r *Runtime) connectorForOperation(code string, operation connector.Operation) (connector.Connector, error) {
	item, err := r.connector(code)
	if err != nil {
		return nil, err
	}
	supported, err := r.registry.Supports(code, operation)
	if err != nil {
		return nil, err
	}
	if !supported {
		return nil, connector.ErrNotSupported
	}
	return item, nil
}

func (r *Runtime) Manifest(code string) (connector.Manifest, error) {
	return r.registry.Manifest(code)
}

func (r *Runtime) Manifests() []connector.Manifest {
	return r.registry.Manifests()
}

func (r *Runtime) Supports(code string, operation connector.Operation) (bool, error) {
	return r.registry.Supports(code, operation)
}

func (r *Runtime) ValidatePaymentMethod(code string, input connector.PaymentMethodMapping) error {
	item, err := r.connector(code)
	if err != nil {
		return err
	}
	return item.ValidatePaymentMethod(input)
}

func (r *Runtime) ValidatePayment(code string, input connector.PaymentValidation) error {
	item, err := r.connector(code)
	if err != nil {
		return err
	}
	return item.ValidatePayment(input)
}

func (r *Runtime) VerifyInstallation(ctx context.Context, input connector.InstallationInput) (connector.InstallationResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationVerifyInstallation)
	if err != nil {
		return connector.InstallationResult{}, err
	}
	return item.VerifyInstallation(ctx, input)
}

func (r *Runtime) DisableInstallation(ctx context.Context, input connector.InstallationInput) error {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationDisableInstallation)
	if err != nil {
		return err
	}
	return item.DisableInstallation(ctx, input)
}

func (r *Runtime) CreatePayment(ctx context.Context, input connector.PaymentInput) (connector.PaymentResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationCreatePayment)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	if err = item.ValidatePayment(connector.PaymentValidation{
		PaymentMethodCode: input.PaymentMethodCode,
		Currency:          input.Currency,
		Amount:            input.Amount,
	}); err != nil {
		return connector.PaymentResult{}, err
	}
	return item.CreatePayment(ctx, input)
}

func (r *Runtime) GetPayment(ctx context.Context, input connector.PaymentLookup) (connector.PaymentResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationGetPayment)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	return item.GetPayment(ctx, input)
}

func (r *Runtime) CapturePayment(ctx context.Context, input connector.CaptureInput) (connector.PaymentResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationCapturePayment)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	return item.CapturePayment(ctx, input)
}

func (r *Runtime) CancelPayment(ctx context.Context, input connector.PaymentLookup, key, reason string) (connector.PaymentResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationCancelPayment)
	if err != nil {
		return connector.PaymentResult{}, err
	}
	return item.CancelPayment(ctx, input, key, reason)
}

func (r *Runtime) SimulatePayment(ctx context.Context, input connector.PaymentLookup, amount int64, currency string) error {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationSimulatePayment)
	if err != nil {
		return err
	}
	return item.SimulatePayment(ctx, input, amount, currency)
}

func (r *Runtime) CreateRefund(ctx context.Context, input connector.RefundInput) (connector.RefundResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationCreateRefund)
	if err != nil {
		return connector.RefundResult{}, err
	}
	return item.CreateRefund(ctx, input)
}

func (r *Runtime) GetRefund(ctx context.Context, input connector.RefundLookup) (connector.RefundResult, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationGetRefund)
	if err != nil {
		return connector.RefundResult{}, err
	}
	return item.GetRefund(ctx, input)
}

func (r *Runtime) HandleWebhook(ctx context.Context, input connector.WebhookInput) (connector.WebhookEvent, error) {
	item, err := r.connectorForOperation(input.ProviderCode, connector.OperationHandleWebhook)
	if err != nil {
		return connector.WebhookEvent{}, err
	}
	return item.HandleWebhook(ctx, input)
}
