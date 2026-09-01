package connectorrunner

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/partnercontract"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const maxRequestBody = 1 << 20

type Engine interface {
	Ping(context.Context) error
	Manifests() []connector.Manifest
	ValidatePaymentMethodVersion(string, string, connector.PaymentMethodMapping) error
	ValidateHostedPaymentMethodsVersion(string, string, []connector.PaymentMethodMapping) error
	ValidatePaymentVersion(string, string, connector.PaymentValidation) error
	VerifyInstallation(context.Context, connector.InstallationInput) (connector.InstallationResult, error)
	DisableInstallation(context.Context, connector.InstallationInput) error
	CreatePayment(context.Context, connector.PaymentInput) (connector.PaymentResult, error)
	GetPayment(context.Context, connector.PaymentLookup) (connector.PaymentResult, error)
	CapturePayment(context.Context, connector.CaptureInput) (connector.PaymentResult, error)
	CancelPayment(context.Context, connector.PaymentLookup, string, string) (connector.PaymentResult, error)
	SimulatePayment(context.Context, connector.PaymentLookup, int64, string) error
	CreateRefund(context.Context, connector.RefundInput) (connector.RefundResult, error)
	GetRefund(context.Context, connector.RefundLookup) (connector.RefundResult, error)
	HandleWebhook(context.Context, connector.WebhookInput) (connector.WebhookEvent, error)
}

type Server struct {
	engine Engine
	token  string
	log    *slog.Logger
}

func New(engine Engine, token string, logger *slog.Logger) (http.Handler, error) {
	if engine == nil {
		return nil, errors.New("connector runner engine is required")
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("connector runner token is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{engine: engine, token: strings.TrimSpace(token), log: logger}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Get("/health/live", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "live"})
	})
	r.Get("/health/ready", s.ready)
	r.Route("/partner/v1", func(r chi.Router) {
		r.Use(s.authenticate)
		r.Get("/health", s.ready)
		r.Get("/capabilities", s.capabilities)
		r.Post("/payment-methods/validate", s.validatePaymentMethod)
		r.Post("/hosted-payment-methods/validate", s.validateHostedPaymentMethods)
		r.Post("/payments/validate", s.validatePayment)
		r.Post("/installations/verify", s.verifyInstallation)
		r.Post("/installations/disable", s.disableInstallation)
		r.Post("/payments/create", s.createPayment)
		r.Post("/payments/get", s.getPayment)
		r.Post("/payments/capture", s.capturePayment)
		r.Post("/payments/cancel", s.cancelPayment)
		r.Post("/payments/simulate", s.simulatePayment)
		r.Post("/refunds/create", s.createRefund)
		r.Post("/refunds/get", s.getRefund)
		r.Post("/webhooks/normalize", s.handleWebhook)
	})
	return r, nil
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if provided == "" || !hmac.Equal([]byte(provided), []byte(s.token)) {
			writeJSON(w, http.StatusUnauthorized, partnercontract.ErrorResponse{Error: partnercontract.Error{Code: "UNAUTHORIZED", Message: "invalid connector runner token"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "connector_count": len(s.engine.Manifests())})
}

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, partnercontract.CapabilitiesResponse{Data: partnercontract.Capabilities{
		ContractVersion: partnercontract.Version,
		Connectors:      s.engine.Manifests(),
	}})
}

func (s *Server) validatePaymentMethod(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProviderCode    string                         `json:"provider_code"`
		ProviderVersion string                         `json:"provider_version,omitempty"`
		Input           connector.PaymentMethodMapping `json:"input"`
	}
	if !decode(w, r, &input) {
		return
	}
	if err := s.engine.ValidatePaymentMethodVersion(input.ProviderCode, input.ProviderVersion, input.Input); err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[map[string]bool]{Data: map[string]bool{"valid": true}})
}

func (s *Server) validateHostedPaymentMethods(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProviderCode    string                           `json:"provider_code"`
		ProviderVersion string                           `json:"provider_version,omitempty"`
		Input           []connector.PaymentMethodMapping `json:"input"`
	}
	if !decode(w, r, &input) {
		return
	}
	if err := s.engine.ValidateHostedPaymentMethodsVersion(input.ProviderCode, input.ProviderVersion, input.Input); err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[map[string]bool]{Data: map[string]bool{"valid": true}})
}

func (s *Server) validatePayment(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProviderCode    string                      `json:"provider_code"`
		ProviderVersion string                      `json:"provider_version,omitempty"`
		Input           connector.PaymentValidation `json:"input"`
	}
	if !decode(w, r, &input) {
		return
	}
	if err := s.engine.ValidatePaymentVersion(input.ProviderCode, input.ProviderVersion, input.Input); err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[map[string]bool]{Data: map[string]bool{"valid": true}})
}

func (s *Server) verifyInstallation(w http.ResponseWriter, r *http.Request) {
	var input connector.InstallationInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.VerifyInstallation(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	// Credential plaintext is already present in the caller's vault workflow;
	// never echo it across the runner response boundary.
	result.StoredCredentials = nil
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.InstallationResult]{Data: result})
}

func (s *Server) disableInstallation(w http.ResponseWriter, r *http.Request) {
	var input connector.InstallationInput
	if !decode(w, r, &input) {
		return
	}
	if err := s.engine.DisableInstallation(r.Context(), input); err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[map[string]bool]{Data: map[string]bool{"disabled": true}})
}

func (s *Server) createPayment(w http.ResponseWriter, r *http.Request) {
	var input connector.PaymentInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.CreatePayment(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.PaymentResult]{Data: result})
}

func (s *Server) getPayment(w http.ResponseWriter, r *http.Request) {
	var input connector.PaymentLookup
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.GetPayment(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.PaymentResult]{Data: result})
}

func (s *Server) capturePayment(w http.ResponseWriter, r *http.Request) {
	var input connector.CaptureInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.CapturePayment(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.PaymentResult]{Data: result})
}

func (s *Server) cancelPayment(w http.ResponseWriter, r *http.Request) {
	var input partnercontract.CancelPaymentRequest
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.CancelPayment(r.Context(), input.Input, input.IdempotencyKey, input.Reason)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.PaymentResult]{Data: result})
}

func (s *Server) simulatePayment(w http.ResponseWriter, r *http.Request) {
	var input partnercontract.SimulatePaymentRequest
	if !decode(w, r, &input) {
		return
	}
	if err := s.engine.SimulatePayment(r.Context(), input.Input, input.Amount, input.Currency); err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[map[string]bool]{Data: map[string]bool{"accepted": true}})
}

func (s *Server) createRefund(w http.ResponseWriter, r *http.Request) {
	var input connector.RefundInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.CreateRefund(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.RefundResult]{Data: result})
}

func (s *Server) getRefund(w http.ResponseWriter, r *http.Request) {
	var input connector.RefundLookup
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.GetRefund(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.RefundResult]{Data: result})
}

func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	var input connector.WebhookInput
	if !decode(w, r, &input) {
		return
	}
	result, err := s.engine.HandleWebhook(r.Context(), input)
	if err != nil {
		s.connectorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, partnercontract.DataResponse[connector.WebhookEvent]{Data: result})
}

func (s *Server) connectorError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	problem := partnercontract.Error{Code: "CONNECTOR_VALIDATION_ERROR", Message: "connector rejected the request"}
	switch {
	case errors.Is(err, connector.ErrOutcomeUnknown):
		status = http.StatusBadGateway
		problem.Code = "CONNECTOR_OUTCOME_UNKNOWN"
		problem.Message = "connector outcome is unknown"
	case errors.Is(err, connector.ErrNotSupported):
		problem.Code = "OPERATION_NOT_SUPPORTED"
		problem.Message = "connector operation is not supported"
	case errors.Is(err, connector.ErrHostedPaymentRestrictionUnsupported):
		problem.Code = "HOSTED_PAYMENT_METHOD_RESTRICTION_UNSUPPORTED"
		problem.Message = err.Error()
	case errors.Is(err, connector.ErrInvalidCredential):
		problem.Code = "INVALID_PROVIDER_CREDENTIAL"
		problem.Message = "provider credential is invalid or its mode cannot be detected"
	default:
		var apiErr *connector.APIError
		if errors.As(err, &apiErr) {
			status = http.StatusBadGateway
			problem.Code = apiErr.Code
			problem.Message = "provider rejected the connector request"
			problem.Provider = apiErr.Provider
			problem.Status = apiErr.Status
		}
	}
	writeJSON(w, status, partnercontract.ErrorResponse{Error: problem})
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, partnercontract.ErrorResponse{Error: partnercontract.Error{Code: "INVALID_JSON", Message: "request body is invalid"}})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, partnercontract.ErrorResponse{Error: partnercontract.Error{Code: "INVALID_JSON", Message: "request body must contain one JSON object"}})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
