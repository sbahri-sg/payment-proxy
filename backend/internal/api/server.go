package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/ids"
	"github.com/emisell/api-payment-proxy/internal/installationservice"
	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/emisell/api-payment-proxy/internal/observability"
	"github.com/emisell/api-payment-proxy/internal/providerapps"
	"github.com/emisell/api-payment-proxy/internal/ratelimit"
	"github.com/emisell/api-payment-proxy/internal/secrets"
	"github.com/emisell/api-payment-proxy/internal/servicekeys"
	"github.com/emisell/api-payment-proxy/internal/store"
	"github.com/emisell/api-payment-proxy/internal/webhooksettings"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	maxRequestBody       = 1 << 20
	maxProviderLogoBytes = 512 << 10
	maxProviderLogoSide  = 2048
	maxAssignmentBatch   = 50
	// apiContractVersion is descriptive metadata returned by readiness and
	// capability resources. Request versioning is defined solely by /api/v1.
	apiContractVersion = "2026-08-28"
)

var (
	tenantPattern        = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	paymentMethodPattern = regexp.MustCompile(`^[a-z0-9_]{2,64}$`)
)

type Engine interface {
	Ping(context.Context) error
	ManifestVersion(string, string) (connector.Manifest, error)
	Manifests() []connector.Manifest
	SupportsVersion(string, string, connector.Operation) (bool, error)
	ValidatePaymentMethodVersion(string, string, connector.PaymentMethodMapping) error
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
	cfg              config.Config
	store            *store.Postgres
	engine           Engine
	cipher           *secrets.Cipher
	installations    *installationservice.Service
	serviceKeys      *servicekeys.Service
	webhookSettings  *webhooksettings.Service
	metrics          *observability.Metrics
	rateLimiter      *ratelimit.Limiter
	adminRateLimiter *ratelimit.Limiter
	inFlight         chan struct{}
	log              *slog.Logger
}

func New(cfg config.Config, database *store.Postgres, engine Engine, cipher *secrets.Cipher, keys *servicekeys.Service, settings *webhooksettings.Service, logger *slog.Logger) http.Handler {
	s := &Server{
		cfg: cfg, store: database, engine: engine, cipher: cipher, serviceKeys: keys,
		installations:   installationservice.New(database, engine, cipher, cfg.PublicBaseURL),
		webhookSettings: settings, metrics: observability.New(),
		rateLimiter: ratelimit.New(cfg.APIRateLimitRPS, cfg.APIRateLimitBurst), log: logger,
		adminRateLimiter: ratelimit.New(cfg.AdminRateLimitRPS, cfg.AdminRateLimitBurst),
	}
	if cfg.APIMaxInFlight > 0 {
		s.inFlight = make(chan struct{}, cfg.APIMaxInFlight)
	}
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, s.securityHeaders, s.responseHeaders, s.accessLog)
	r.Get("/health/live", s.live)
	r.Get("/health/ready", s.ready)
	r.Post("/webhooks/v1/providers/{provider}/{installationID}", s.providerWebhook)
	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/admin", func(r chi.Router) {
			r.Use(s.protectAdminTraffic, s.authenticateAdmin, s.requestDeadline)
			s.registerAdminRoutes(r)
		})
		r.Group(func(r chi.Router) {
			r.Use(s.authenticateService, s.protectServiceTraffic, s.requestDeadline)
			r.Get("/engine/capabilities", s.engineCapabilities)
			r.Get("/integration-readiness", s.integrationReadiness)
			r.Get("/providers", s.listProviders)
			r.Get("/provider-assets/{providerCode}/logo", s.providerLogo)
			r.Get("/payment-methods", s.listPaymentMethods)
			r.Get("/provider-installations", s.listInstallations)
			r.Post("/provider-installations", s.createInstallation)
			r.Get("/provider-installations/{id}", s.getInstallation)
			r.Put("/provider-installations/{id}/credentials", s.configureCredentials)
			r.Patch("/provider-installations/{id}/credentials", s.configureCredentials)
			r.Post("/provider-installations/{id}/upgrade", s.upgradeInstallation)
			r.Post("/provider-installations/{id}/activate", s.activateInstallation)
			r.Post("/provider-installations/{id}/deactivate", s.deactivateInstallation)
			r.Delete("/provider-installations/{id}", s.uninstallInstallation)
			r.Get("/payment-method-assignments", s.listPaymentMethodAssignments)
			r.Put("/payment-method-assignments", s.upsertPaymentMethodAssignment)
			r.Post("/payment-method-assignments/{id}/deactivate", s.deactivatePaymentMethodAssignment)
			r.Get("/payment-options", s.listPaymentOptions)
			r.Get("/payment-sessions", s.listPayments)
			r.Post("/payment-sessions", s.createPayment)
			r.Get("/payment-sessions/{id}", s.getPayment)
			r.Get("/payment-sessions/{id}/timeline", s.paymentTimeline)
			r.Post("/payment-sessions/{id}/cancel", s.cancelPayment)
			r.Get("/webhook-inbox", s.listWebhookInbox)
			r.Get("/webhook-deliveries", s.listWebhookDeliveries)
			r.Post("/webhook-deliveries/{id}/replay", s.replayWebhookDelivery)
			r.Get("/reconciliation/cases", s.listReconciliationCases)
			r.Post("/reconciliation/payments/{id}/resolve", s.resolvePaymentReconciliation)
			r.Post("/refunds", s.createRefund)
			r.Get("/refunds/{id}", s.getRefund)
		})
	})
	return r
}

func (s *Server) registerAdminRoutes(r chi.Router) {
	r.Get("/dashboard/overview", s.dashboardOverview)
	r.Get("/engine/readiness", s.engineReadiness)
	r.Get("/observability", s.observabilitySnapshot)
	r.Get("/metrics", s.prometheusMetrics)
	r.Get("/service-api-keys", s.listServiceAPIKeys)
	r.Post("/service-api-keys", s.createServiceAPIKey)
	r.Post("/service-api-keys/{id}/revoke", s.revokeServiceAPIKey)
	r.Get("/provider-apps", s.listProviderApps)
	r.Post("/provider-apps/{id}/transition", s.transitionProviderApp)
	r.Get("/provider-app-providers", s.listProviderAppProviders)
	r.Post("/provider-app-providers", s.createProviderAppProvider)
	r.Get("/provider-app-providers/{providerCode}", s.getProviderAppProvider)
	r.Post("/provider-app-providers/{providerCode}/transition", s.transitionProviderAppProvider)
	r.Get("/provider-app-providers/{providerCode}/versions", s.listProviderAppVersions)
	r.Post("/provider-app-providers/{providerCode}/versions", s.uploadProviderAppVersion)
	r.Get("/connector-certifications", s.listConnectorCertifications)
	r.Post("/connector-certifications/run", s.runConnectorCertification)
	r.Get("/emisell-webhook", s.getEmisellWebhookSettings)
	r.Put("/emisell-webhook", s.updateEmisellWebhookSettings)
	r.Post("/emisell-webhook/secret", s.generateEmisellWebhookSecret)
	r.Post("/emisell-webhook/test", s.testEmisellWebhook)
}

func (s *Server) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "payment-proxy"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{"database": "ok", "emisell_engine": "ok"}
	ready := true
	if err := s.store.Ping(ctx); err != nil {
		checks["database"] = "unavailable"
		ready = false
	}
	if err := s.engine.Ping(ctx); err != nil {
		checks["emisell_engine"] = "unavailable"
		ready = false
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"status": map[bool]string{true: "ready", false: "not_ready"}[ready], "checks": checks})
}

func (s *Server) engineCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, map[string]any{
		"engine":             "emisell_payment_engine",
		"contract_version":   apiContractVersion,
		"connector_contract": "v1",
		"selection_mode":     "merchant_installation",
		"unknown_policy":     "reconcile_same_provider",
		"connectors":         s.engine.Manifests(),
		"integration_invariants": []string{
			"hosted_checkout_uses_provider_redirect_url",
			"payment_is_pinned_to_one_installation",
			"unknown_outcome_never_fails_over",
			"provider_credentials_never_leave_payment_platform",
		},
	})
}

func (s *Server) integrationReadiness(w http.ResponseWriter, r *http.Request) {
	mode, ok := requireMode(w, r)
	if !ok {
		return
	}
	facts, err := s.store.GetIntegrationReadinessFacts(r.Context(), tenant(r), mode)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	settings, err := s.webhookSettings.Get(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	webhookConfigured := settings.Configured && settings.Enabled && settings.SecretConfigured
	checks := make([]model.IntegrationReadinessCheck, 0, 6)
	add := func(code, label string, passed bool, pendingDetail string) {
		status, detail := "PASSED", "Verified from platform evidence."
		if !passed {
			status, detail = "PENDING", pendingDetail
		}
		checks = append(checks, model.IntegrationReadinessCheck{Code: code, Label: label, Status: status, Detail: detail})
	}
	add("provider_connection", "Active provider connection", facts.ActiveInstallation, "Install, configure, verify, and activate a provider for this environment.")
	add("payment_create", "Payment creation", facts.PaymentCreated, "Create one payment session using a stable Idempotency-Key.")
	add("idempotency_replay", "Idempotency replay", facts.IdempotencyReplay, "Repeat the same payment request with the same Idempotency-Key and confirm the same payment is returned.")
	add("payment_status", "Payment status lookup", facts.PaymentStatusRead, "Retrieve the created payment by its canonical payment ID.")
	add("backend_webhook", "Emisell Backend webhook", webhookConfigured, "Configure and enable the signed Emisell Backend webhook in Admin settings.")
	add("webhook_delivery", "Successful webhook delivery", facts.WebhookDelivered, "Complete a sandbox payment and return HTTP 2xx from the Emisell Backend webhook receiver.")

	result := model.IntegrationReadiness{
		Environment: mode, Status: "READY", Total: len(checks),
		ResilienceEvidence: facts.ResilienceObserved, Checks: checks,
	}
	for _, check := range checks {
		if check.Status == "PASSED" {
			result.Passed++
			continue
		}
		if result.RecommendedAction == "" {
			result.RecommendedAction = check.Detail
		}
	}
	if result.Passed != result.Total {
		result.Status = "NOT_READY"
	}
	writeData(w, http.StatusOK, result)
}

func (s *Server) engineReadiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{
		"database":              "ok",
		"connector_registry":    "ok",
		"runtime_configuration": "ok",
	}
	ready := true
	if err := s.store.Ping(ctx); err != nil {
		checks["database"] = "unavailable"
		ready = false
	}
	if err := s.engine.Ping(ctx); err != nil {
		checks["connector_registry"] = "unavailable"
		ready = false
	}
	if err := s.cfg.ValidateRuntime(); err != nil {
		checks["runtime_configuration"] = "invalid"
		ready = false
	}
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	writeData(w, status, map[string]any{
		"status":           map[bool]string{true: "ready", false: "not_ready"}[ready],
		"environment":      s.cfg.AppEnv,
		"contract_version": apiContractVersion,
		"connector_count":  len(s.engine.Manifests()),
		"checks":           checks,
		"request_guards": map[string]any{
			"max_body_bytes":         maxRequestBody,
			"timeout_seconds":        int(s.cfg.APIRequestTimeout.Seconds()),
			"rate_limit_rps":         s.cfg.APIRateLimitRPS,
			"rate_limit_burst":       s.cfg.APIRateLimitBurst,
			"admin_rate_limit_rps":   s.cfg.AdminRateLimitRPS,
			"admin_rate_limit_burst": s.cfg.AdminRateLimitBurst,
			"max_in_flight":          s.cfg.APIMaxInFlight,
			"rate_limit_scope":       "per_replica_per_merchant",
		},
	})
}

func (s *Server) observabilitySnapshot(w http.ResponseWriter, _ *http.Request) {
	writeData(w, http.StatusOK, s.metrics.Snapshot())
}

func (s *Server) prometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, s.metrics.Prometheus())
}

func (s *Server) dashboardOverview(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.DashboardOverview(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

type createServiceAPIKeyRequest struct {
	Name string `json:"name"`
}

func (s *Server) listServiceAPIKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.serviceKeys.List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) createServiceAPIKey(w http.ResponseWriter, r *http.Request) {
	var request createServiceAPIKeyRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Name = strings.Join(strings.Fields(request.Name), " ")
	if len(request.Name) < 3 || len(request.Name) > 80 {
		problem(w, http.StatusUnprocessableEntity, "INVALID_API_KEY_NAME", "name must contain 3 to 80 characters")
		return
	}
	generated, err := s.serviceKeys.Generate(r.Context(), request.Name, actor(r), middleware.GetReqID(r.Context()))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusCreated, generated)
}

func (s *Server) revokeServiceAPIKey(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !strings.HasPrefix(id, "sak_") {
		problem(w, http.StatusNotFound, "API_KEY_NOT_FOUND", "service API key was not found or is already revoked")
		return
	}
	item, err := s.serviceKeys.Revoke(r.Context(), id, actor(r), middleware.GetReqID(r.Context()))
	if errors.Is(err, servicekeys.ErrNotFound) {
		problem(w, http.StatusNotFound, "API_KEY_NOT_FOUND", "service API key was not found or is already revoked")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listProviderApps(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListProviderApps(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listProviderAppProviders(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListProviderAppProviders(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getProviderAppProvider(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetProviderAppProvider(r.Context(), strings.ToLower(strings.TrimSpace(chi.URLParam(r, "providerCode"))))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listProviderAppVersions(w http.ResponseWriter, r *http.Request) {
	providerCode := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "providerCode")))
	if _, err := s.store.GetProviderAppProvider(r.Context(), providerCode); err != nil {
		s.storeError(w, r, err)
		return
	}
	items, err := s.store.ListProviderAppsByProvider(r.Context(), providerCode)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

type createProviderAppProviderRequest struct {
	ProviderCode     string `json:"provider_code"`
	ProviderName     string `json:"provider_name"`
	Description      string `json:"description"`
	WebsiteURL       string `json:"website_url"`
	DocumentationURL string `json:"documentation_url"`
	SupportEmail     string `json:"support_email"`
}

func (s *Server) createProviderAppProvider(w http.ResponseWriter, r *http.Request) {
	var request createProviderAppProviderRequest
	var logo []byte
	var logoContentType string
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		if err := r.ParseMultipartForm(maxRequestBody); err != nil {
			problem(w, http.StatusBadRequest, "INVALID_PROVIDER_FORM", "provider form is invalid or too large")
			return
		}
		request = createProviderAppProviderRequest{
			ProviderCode: r.FormValue("provider_code"), ProviderName: r.FormValue("provider_name"),
			Description: r.FormValue("description"), WebsiteURL: r.FormValue("website_url"),
			DocumentationURL: r.FormValue("documentation_url"), SupportEmail: r.FormValue("support_email"),
		}
		file, header, err := r.FormFile("logo")
		if err != nil && !errors.Is(err, http.ErrMissingFile) {
			problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_LOGO", "provider logo could not be read")
			return
		}
		if err == nil {
			defer file.Close()
			logo, err = io.ReadAll(io.LimitReader(file, maxProviderLogoBytes+1))
			if err != nil {
				problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_LOGO", "provider logo could not be read")
				return
			}
			logoContentType, err = validateProviderLogo(logo, header.Header.Get("Content-Type"))
			if err != nil {
				problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_LOGO", err.Error())
				return
			}
		}
	} else if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.ProviderCode = strings.ToLower(strings.TrimSpace(request.ProviderCode))
	request.ProviderName = strings.Join(strings.Fields(request.ProviderName), " ")
	request.Description = strings.TrimSpace(request.Description)
	request.WebsiteURL = strings.TrimSpace(request.WebsiteURL)
	request.DocumentationURL = strings.TrimSpace(request.DocumentationURL)
	request.SupportEmail = strings.ToLower(strings.TrimSpace(request.SupportEmail))
	if !regexp.MustCompile(`^[a-z0-9_-]{2,48}$`).MatchString(request.ProviderCode) || len(request.ProviderName) < 2 || len(request.ProviderName) > 120 || len(request.Description) > 500 {
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_PROFILE", "provider code, name, or description is invalid")
		return
	}
	if !optionalPublicHTTPSURL(request.WebsiteURL) || !optionalPublicHTTPSURL(request.DocumentationURL) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_URL", "website_url and documentation_url must be public HTTPS URLs")
		return
	}
	if request.SupportEmail != "" {
		address, err := mail.ParseAddress(request.SupportEmail)
		if err != nil || !strings.EqualFold(address.Address, request.SupportEmail) || len(request.SupportEmail) > 254 {
			problem(w, http.StatusUnprocessableEntity, "INVALID_SUPPORT_EMAIL", "support_email must be a valid email address")
			return
		}
	}
	item, err := s.store.CreateProviderAppProvider(r.Context(), store.CreateProviderAppProviderInput{
		ProviderCode: request.ProviderCode, ProviderName: request.ProviderName, Description: request.Description,
		WebsiteURL: request.WebsiteURL, DocumentationURL: request.DocumentationURL, SupportEmail: request.SupportEmail,
		Logo: logo, LogoContentType: logoContentType, Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

func validateProviderLogo(data []byte, declaredContentType string) (string, error) {
	if len(data) == 0 {
		return "", errors.New("provider logo is empty")
	}
	if len(data) > maxProviderLogoBytes {
		return "", errors.New("provider logo must not exceed 512 KB")
	}
	detected := http.DetectContentType(data)
	declared := strings.ToLower(strings.TrimSpace(strings.Split(declaredContentType, ";")[0]))
	if declared == "image/jpg" {
		declared = "image/jpeg"
	}
	if declared != "" && declared != "application/octet-stream" && declared != detected {
		return "", errors.New("provider logo content does not match its file type")
	}
	var width, height int
	switch detected {
	case "image/png":
		config, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", errors.New("provider logo is not a valid PNG image")
		}
		width, height = config.Width, config.Height
	case "image/jpeg":
		config, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", errors.New("provider logo is not a valid JPEG image")
		}
		width, height = config.Width, config.Height
	default:
		return "", errors.New("provider logo must be a PNG or JPEG image")
	}
	if width < 1 || height < 1 || width > maxProviderLogoSide || height > maxProviderLogoSide {
		return "", errors.New("provider logo dimensions must be between 1 and 2048 pixels")
	}
	return detected, nil
}

func (s *Server) providerLogo(w http.ResponseWriter, r *http.Request) {
	providerCode := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "providerCode")))
	if !regexp.MustCompile(`^[a-z0-9_-]{2,48}$`).MatchString(providerCode) {
		problem(w, http.StatusNotFound, "PROVIDER_LOGO_NOT_FOUND", "provider logo was not found")
		return
	}
	logo, contentType, err := s.store.GetProviderLogo(r.Context(), providerCode)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			problem(w, http.StatusNotFound, "PROVIDER_LOGO_NOT_FOUND", "provider logo was not found")
			return
		}
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(logo)
}

type providerAppProviderTransitionRequest struct {
	ExpectedStatus string `json:"expected_status"`
	Status         string `json:"status"`
}

func (s *Server) transitionProviderAppProvider(w http.ResponseWriter, r *http.Request) {
	providerCode := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "providerCode")))
	var request providerAppProviderTransitionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.ExpectedStatus = strings.ToUpper(strings.TrimSpace(request.ExpectedStatus))
	request.Status = strings.ToUpper(strings.TrimSpace(request.Status))
	if !regexp.MustCompile(`^[a-z0-9_-]{2,48}$`).MatchString(providerCode) ||
		!validProviderRegistryStatus(request.ExpectedStatus) || !validProviderRegistryStatus(request.Status) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_STATUS", "provider code, expected_status, or status is invalid")
		return
	}
	item, err := s.store.TransitionProviderAppProvider(r.Context(), store.TransitionProviderAppProviderInput{
		ProviderCode: providerCode, ExpectedStatus: request.ExpectedStatus, Status: request.Status,
		Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func validProviderRegistryStatus(status string) bool {
	return status == "DRAFT" || status == "ACTIVE" || status == "DISABLED"
}

func optionalPublicHTTPSURL(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsUnspecified() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
	}
	return true
}

func (s *Server) uploadProviderAppVersion(w http.ResponseWriter, r *http.Request) {
	providerCode := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "providerCode")))
	if !regexp.MustCompile(`^[a-z0-9_-]{2,48}$`).MatchString(providerCode) {
		problem(w, http.StatusNotFound, "PROVIDER_NOT_FOUND", "Provider App provider was not found")
		return
	}
	s.uploadProviderAppForProvider(w, r, providerCode)
}

func (s *Server) uploadProviderAppForProvider(w http.ResponseWriter, r *http.Request, expectedProviderCode string) {
	r.Body = http.MaxBytesReader(w, r.Body, providerapps.MaxArtifactBytes+(1<<20))
	if err := r.ParseMultipartForm(providerapps.MaxArtifactBytes + (1 << 20)); err != nil {
		problem(w, http.StatusBadRequest, "INVALID_PROVIDER_APP_UPLOAD", "multipart upload is invalid or exceeds 25 MB")
		return
	}
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	file, header, err := r.FormFile("bundle")
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "PROVIDER_APP_BUNDLE_REQUIRED", "bundle ZIP file is required")
		return
	}
	defer file.Close()
	fileName := filepath.Base(strings.TrimSpace(header.Filename))
	if len(fileName) < 5 || len(fileName) > 255 || !strings.EqualFold(filepath.Ext(fileName), ".zip") {
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_APP_FILE", "bundle must use a valid .zip file name")
		return
	}
	payload, err := io.ReadAll(io.LimitReader(file, providerapps.MaxArtifactBytes+1))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if len(payload) > providerapps.MaxArtifactBytes {
		problem(w, http.StatusRequestEntityTooLarge, "PROVIDER_APP_TOO_LARGE", "provider app bundle exceeds 25 MB")
		return
	}
	result, err := providerapps.ValidateBundle(payload)
	if err != nil {
		status := http.StatusUnprocessableEntity
		code := "INVALID_PROVIDER_APP_BUNDLE"
		if errors.Is(err, providerapps.ErrBundleTooLarge) {
			status = http.StatusRequestEntityTooLarge
			code = "PROVIDER_APP_TOO_LARGE"
		}
		problem(w, status, code, err.Error())
		return
	}
	if expectedProviderCode != "" && result.Manifest.Code != expectedProviderCode {
		clear(payload)
		problem(w, http.StatusUnprocessableEntity, "PROVIDER_IDENTITY_MISMATCH", "bundle manifest provider code does not match the selected provider")
		return
	}
	registeredProvider, err := s.store.GetProviderAppProvider(r.Context(), result.Manifest.Code)
	if errors.Is(err, store.ErrNotFound) {
		clear(payload)
		problem(w, http.StatusConflict, "PROVIDER_NOT_REGISTERED", "create the Provider App provider before uploading a connector version")
		return
	}
	if err != nil {
		clear(payload)
		s.internal(w, r, err)
		return
	}
	if registeredProvider.Status == "DISABLED" || !strings.EqualFold(registeredProvider.ProviderName, result.Manifest.Name) {
		clear(payload)
		problem(w, http.StatusConflict, "PROVIDER_IDENTITY_MISMATCH", "bundle manifest name and code must match the locked provider identity")
		return
	}
	manifestJSON, err := json.Marshal(result.Manifest)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	reportJSON, err := json.Marshal(result.Report)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	id, ok := s.newID(w, r, "papp")
	if !ok {
		return
	}
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/zip"
	}
	item, err := s.store.CreateProviderApp(r.Context(), store.CreateProviderAppInput{
		ID: id, ProviderCode: result.Manifest.Code, ProviderName: result.Manifest.Name,
		Version: result.Manifest.Version, Runtime: result.Manifest.Runtime, SDKVersion: result.Manifest.SDKVersion,
		FileName: fileName, ContentType: contentType, ArtifactSHA256: result.ArtifactSHA256,
		Artifact: payload, Manifest: manifestJSON, ScanReport: reportJSON,
		Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	clear(payload)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, item)
}

type providerAppTransitionRequest struct {
	ExpectedStatus   string `json:"expected_status"`
	Status           string `json:"status"`
	LegacyReviewNote string `json:"review_note,omitempty"` // accepted for older admin clients; ignored
}

func (s *Server) transitionProviderApp(w http.ResponseWriter, r *http.Request) {
	var request providerAppTransitionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.ExpectedStatus = strings.ToUpper(strings.TrimSpace(request.ExpectedStatus))
	request.Status = strings.ToUpper(strings.TrimSpace(request.Status))
	item, err := s.store.GetProviderApp(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if item.Status != request.ExpectedStatus {
		problem(w, http.StatusConflict, "PROVIDER_APP_STATUS_CONFLICT", "provider app status has changed")
		return
	}
	runtimeDigest := ""
	verificationReport := "{}"
	verifiedCapabilities := []string(nil)
	reviewNote := ""
	switch request.Status {
	case "VALIDATED":
		artifact, artifactErr := s.store.GetProviderAppArtifact(r.Context(), item.ID)
		if artifactErr != nil {
			s.storeError(w, r, artifactErr)
			return
		}
		validation, validationErr := providerapps.ValidateBundle(artifact)
		clear(artifact)
		if validationErr != nil || validation.ArtifactSHA256 != item.ArtifactSHA256 || validation.Manifest.Code != item.ProviderCode || validation.Manifest.Version != item.Version {
			problem(w, http.StatusConflict, "PROVIDER_APP_VALIDATION_FAILED", "stored artifact no longer matches its validated manifest")
			return
		}
	case "CERTIFIED":
		report, verificationErr := s.verifyProviderAppRelease(r.Context(), item)
		if verificationErr != nil {
			problem(w, http.StatusConflict, "PROVIDER_APP_VERIFICATION_FAILED", verificationErr.Error())
			return
		}
		report.VerifiedAt = time.Now().UTC().Format(time.RFC3339)
		payload, marshalErr := json.Marshal(report)
		if marshalErr != nil {
			s.internal(w, r, marshalErr)
			return
		}
		verificationReport = string(payload)
		reviewNote = fmt.Sprintf("Automated backend verification passed for %d capabilities on runtime %s.", len(report.VerifiedCapabilities), report.RuntimeVersion)
	case "PUBLISHED":
		runtimeManifest, manifestErr := s.engine.ManifestVersion(item.ProviderCode, item.Version)
		if manifestErr != nil {
			problem(w, http.StatusConflict, "CONNECTOR_RUNTIME_NOT_READY", "deploy the isolated connector runtime with this exact manifest version before publish")
			return
		}
		var storedVerification providerapps.ReleaseVerificationReport
		if err := json.Unmarshal(item.VerificationReport, &storedVerification); err != nil || !storedVerification.Passed {
			problem(w, http.StatusConflict, "PROVIDER_APP_NOT_VERIFIED", "run backend release verification before publish")
			return
		}
		if storedVerification.Source == "automated_backend_verification" &&
			(storedVerification.RuntimeVersion != runtimeManifest.Version || storedVerification.RuntimeDigest != runtimeManifest.ExecutableSHA256) {
			problem(w, http.StatusConflict, "PROVIDER_APP_VERIFICATION_STALE", "shared runtime identity changed after verification; restore the verified digest or submit a new version")
			return
		}
		verifiedCapabilities = append(verifiedCapabilities, storedVerification.VerifiedCapabilities...)
		var scanReport providerapps.ScanReport
		if err := json.Unmarshal(item.ScanReport, &scanReport); err != nil || runtimeManifest.ExecutableSHA256 == "" {
			problem(w, http.StatusConflict, "CONNECTOR_ARTIFACT_MISMATCH", "the deployed connector executable does not match the verified Provider App artifact")
			return
		}
		switch scanReport.PackageFormat {
		case providerapps.PackageFormatSubmissionV1:
			// The review ZIP is deliberately source-only. The separately deployed
			// runtime identity is pinned in provider_versions at publish time.
		case "", providerapps.PackageFormatLegacyBundle:
			if scanReport.EntrypointSHA256 == "" || !hmac.Equal([]byte(scanReport.EntrypointSHA256), []byte(runtimeManifest.ExecutableSHA256)) {
				problem(w, http.StatusConflict, "CONNECTOR_ARTIFACT_MISMATCH", "the deployed connector executable does not match the verified legacy Provider App artifact")
				return
			}
		default:
			problem(w, http.StatusConflict, "PROVIDER_APP_FORMAT_UNSUPPORTED", "the certified Provider App package format is not supported")
			return
		}
		runtimeDigest = runtimeManifest.ExecutableSHA256
		reviewNote = fmt.Sprintf("Automated publish of verified runtime %s with immutable digest %s.", runtimeManifest.Version, runtimeDigest)
	case "DEPRECATED", "DISABLED":
	default:
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER_APP_STATUS", "unsupported provider app transition")
		return
	}
	updated, err := s.store.TransitionProviderApp(r.Context(), store.ProviderAppTransitionInput{
		ID: item.ID, ExpectedStatus: request.ExpectedStatus, Status: request.Status,
		ReviewNote: reviewNote, RuntimeDigest: runtimeDigest, VerificationReport: verificationReport,
		VerifiedCapabilities: verifiedCapabilities, Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, updated)
}

func (s *Server) verifyProviderAppRelease(ctx context.Context, item model.ProviderAppVersion) (providerapps.ReleaseVerificationReport, error) {
	artifact, err := s.store.GetProviderAppArtifact(ctx, item.ID)
	if err != nil {
		return providerapps.ReleaseVerificationReport{}, fmt.Errorf("stored provider release artifact is unavailable")
	}
	validation, err := providerapps.ValidateBundle(artifact)
	clear(artifact)
	if err != nil || validation.ArtifactSHA256 != item.ArtifactSHA256 || validation.Manifest.Code != item.ProviderCode || validation.Manifest.Version != item.Version {
		return providerapps.ReleaseVerificationReport{}, fmt.Errorf("stored provider release no longer matches its validated bundle")
	}
	runtimeManifest, err := s.engine.ManifestVersion(item.ProviderCode, item.Version)
	if err != nil {
		return providerapps.ReleaseVerificationReport{}, fmt.Errorf("shared runtime %s@%s is not loaded", item.ProviderCode, item.Version)
	}
	report := providerapps.VerifyRuntimeContract(validation.Manifest, runtimeManifest)
	for _, paymentMethodCode := range validation.Manifest.PaymentMethods {
		capability, capabilityErr := s.store.GetProviderPaymentMethodCapability(ctx, item.ProviderCode, paymentMethodCode)
		if capabilityErr != nil {
			report.AddCheck("mapping:"+paymentMethodCode, false, "canonical catalog mapping is missing")
			continue
		}
		mappingErr := s.engine.ValidatePaymentMethodVersion(item.ProviderCode, item.Version, connector.PaymentMethodMapping{
			PaymentMethodCode:  capability.PaymentMethodCode,
			ProviderMethod:     capability.ProviderMethod,
			ProviderMethodType: capability.ProviderMethodType,
		})
		if mappingErr != nil {
			report.AddCheck("mapping:"+paymentMethodCode, false, mappingErr.Error())
			continue
		}
		report.AddCheck("mapping:"+paymentMethodCode, true, capability.ProviderMethod+"/"+capability.ProviderMethodType)
		report.VerifiedCapabilities = append(report.VerifiedCapabilities, paymentMethodCode)
	}
	if !report.Passed {
		return report, fmt.Errorf("backend verification found a release/runtime contract mismatch")
	}
	sort.Strings(report.VerifiedCapabilities)
	return report, nil
}

func (s *Server) getEmisellWebhookSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.webhookSettings.Get(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, settings)
}

type emisellWebhookSettingsRequest struct {
	CallbackURL string `json:"callback_url"`
	Enabled     bool   `json:"enabled"`
}

func (s *Server) updateEmisellWebhookSettings(w http.ResponseWriter, r *http.Request) {
	var request emisellWebhookSettingsRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	settings, err := s.webhookSettings.Update(r.Context(), request.CallbackURL, request.Enabled, actor(r))
	if err != nil {
		s.webhookSettingsError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, settings)
}

func (s *Server) generateEmisellWebhookSecret(w http.ResponseWriter, r *http.Request) {
	generated, err := s.webhookSettings.GenerateSecret(r.Context(), actor(r))
	if err != nil {
		s.webhookSettingsError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusCreated, generated)
}

func (s *Server) testEmisellWebhook(w http.ResponseWriter, r *http.Request) {
	result, err := s.webhookSettings.Test(r.Context(), actor(r))
	if err != nil {
		s.webhookSettingsError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeData(w, http.StatusOK, result)
}

func (s *Server) webhookSettingsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, webhooksettings.ErrInvalidURL):
		problem(w, http.StatusUnprocessableEntity, "INVALID_CALLBACK_URL", "callback URL must be an allowed HTTP(S) Emisell Backend endpoint")
	case errors.Is(err, webhooksettings.ErrSecretNotConfigured):
		problem(w, http.StatusConflict, "WEBHOOK_SECRET_REQUIRED", "generate a webhook secret before enabling or testing delivery")
	case errors.Is(err, webhooksettings.ErrNotConfigured):
		problem(w, http.StatusConflict, "WEBHOOK_NOT_CONFIGURED", "callback URL and webhook secret must be configured")
	default:
		s.internal(w, r, err)
	}
}

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	search, ok := catalogSearchQuery(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListProviders(r.Context(), search)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listPaymentMethods(w http.ResponseWriter, r *http.Request) {
	search, ok := catalogSearchQuery(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListPaymentMethods(r.Context(), search)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listInstallations(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListInstallations(r.Context(), tenant(r), optionalMode(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	for index := range items {
		items[index] = s.withProviderWebhookURL(items[index])
	}
	writeData(w, http.StatusOK, items)
}
func (s *Server) getInstallation(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetInstallation(r.Context(), tenant(r), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, s.withProviderWebhookURL(item))
}

type createInstallationRequest struct {
	ProviderCode    string `json:"provider_code"`
	ProviderVersion string `json:"provider_version"`
	Environment     string `json:"environment"`
}

func (s *Server) createInstallation(w http.ResponseWriter, r *http.Request) {
	var request createInstallationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	item, err := s.installations.Create(r.Context(), installationservice.CreateInput{
		TenantID: tenant(r), ProviderCode: request.ProviderCode,
		ProviderVersion: request.ProviderVersion, Environment: request.Environment,
		Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.installationError(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, s.withProviderWebhookURL(item))
}

type credentialRequest struct {
	Credentials    map[string]string `json:"credentials"`
	PaymentMethods []map[string]any  `json:"payment_methods"`
	ClearFields    []string          `json:"clear_fields,omitempty"`
}

func (s *Server) configureCredentials(w http.ResponseWriter, r *http.Request) {
	var request credentialRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	defer clearCredentials(request.Credentials)
	installation, err := s.installations.Configure(r.Context(), installationservice.ConfigureInput{
		TenantID: tenant(r), InstallationID: chi.URLParam(r, "id"),
		Credentials: request.Credentials, ClearFields: request.ClearFields,
		PaymentMethods: request.PaymentMethods, PaymentMethodsPresent: request.PaymentMethods != nil,
		Patch: r.Method == http.MethodPatch, Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.installationError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, s.withProviderWebhookURL(installation))
}

type versionRequest struct {
	Version int64 `json:"version"`
}

type upgradeInstallationRequest struct {
	Version         int64  `json:"version"`
	ProviderVersion string `json:"provider_version"`
}

func (s *Server) upgradeInstallation(w http.ResponseWriter, r *http.Request) {
	var request upgradeInstallationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	item, err := s.installations.Upgrade(r.Context(), installationservice.UpgradeInput{
		TenantID: tenant(r), InstallationID: chi.URLParam(r, "id"),
		ProviderVersion: request.ProviderVersion, ExpectedVersion: request.Version,
		Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.installationError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, s.withProviderWebhookURL(item))
}

func (s *Server) activateInstallation(w http.ResponseWriter, r *http.Request) {
	s.transitionInstallation(w, r, model.InstallationActive)
}
func (s *Server) deactivateInstallation(w http.ResponseWriter, r *http.Request) {
	s.transitionInstallation(w, r, model.InstallationInactive)
}
func (s *Server) transitionInstallation(w http.ResponseWriter, r *http.Request, target string) {
	var request versionRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			return
		}
	}
	input := installationservice.TransitionInput{
		TenantID: tenant(r), InstallationID: chi.URLParam(r, "id"),
		Actor: actor(r), RequestID: middleware.GetReqID(r.Context()), ExpectedVersion: request.Version,
	}
	var item model.Installation
	var err error
	if target == model.InstallationActive {
		item, err = s.installations.Activate(r.Context(), input)
	} else {
		item, err = s.installations.Deactivate(r.Context(), input)
	}
	if err != nil {
		s.installationError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, s.withProviderWebhookURL(item))
}
func (s *Server) uninstallInstallation(w http.ResponseWriter, r *http.Request) {
	item, err := s.installations.Uninstall(r.Context(), installationservice.UninstallInput{
		TenantID: tenant(r), InstallationID: chi.URLParam(r, "id"),
		Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.installationError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, s.withProviderWebhookURL(item))
}

func (s *Server) listPaymentMethodAssignments(w http.ResponseWriter, r *http.Request) {
	environment, ok := optionalEnvironmentQuery(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListPaymentMethodAssignments(r.Context(), tenant(r), environment)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listPaymentOptions(w http.ResponseWriter, r *http.Request) {
	environment, ok := requireEnvironmentQuery(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListPaymentOptions(r.Context(), tenant(r), environment)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

type paymentMethodAssignmentRequest struct {
	PaymentMethodCode string `json:"payment_method_code"`
	InstallationID    string `json:"installation_id"`
	PaymentMethod     string `json:"payment_method"`
	PaymentMethodType string `json:"payment_method_type"`
	// LegacyLabel is accepted during the v1 compatibility window but ignored.
	// Checkout labels always come from the canonical payment-method catalog.
	LegacyLabel string `json:"label"`
	Version     int64  `json:"version"`
}

type paymentMethodAssignmentsRequest struct {
	Assignments       *[]paymentMethodAssignmentRequest `json:"assignments"`
	PaymentMethodCode *string                           `json:"payment_method_code"`
	InstallationID    *string                           `json:"installation_id"`
	PaymentMethod     *string                           `json:"payment_method"`
	PaymentMethodType *string                           `json:"payment_method_type"`
	LegacyLabel       *string                           `json:"label"`
	Version           *int64                            `json:"version"`
}

func (s *Server) upsertPaymentMethodAssignment(w http.ResponseWriter, r *http.Request) {
	requests, legacy, ok := decodePaymentMethodAssignmentRequests(w, r)
	if !ok {
		return
	}
	inputs := make([]store.UpsertPaymentMethodAssignmentInput, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for index := range requests {
		input, prepared := s.preparePaymentMethodAssignment(w, r, requests[index])
		if !prepared {
			return
		}
		key := input.Environment + "\x00" + input.PaymentMethodCode
		if _, duplicate := seen[key]; duplicate {
			problem(w, http.StatusUnprocessableEntity, "DUPLICATE_ASSIGNMENT", "assignments must not repeat a payment_method_code in the same environment")
			return
		}
		seen[key] = struct{}{}
		inputs = append(inputs, input)
	}
	items, created, err := s.store.UpsertPaymentMethodAssignments(r.Context(), inputs)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if legacy {
		status := http.StatusOK
		if created == 1 {
			status = http.StatusCreated
		}
		writeData(w, status, items[0])
		return
	}
	writeData(w, http.StatusOK, items)
}

func decodePaymentMethodAssignmentRequests(w http.ResponseWriter, r *http.Request) ([]paymentMethodAssignmentRequest, bool, bool) {
	var payload paymentMethodAssignmentsRequest
	if err := decodeJSON(w, r, &payload); err != nil {
		return nil, false, false
	}
	legacyFieldsPresent := payload.PaymentMethodCode != nil || payload.InstallationID != nil || payload.PaymentMethod != nil || payload.PaymentMethodType != nil || payload.LegacyLabel != nil || payload.Version != nil
	if payload.Assignments != nil {
		if legacyFieldsPresent {
			problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "assignments cannot be combined with legacy single-assignment fields")
			return nil, false, false
		}
		if len(*payload.Assignments) == 0 || len(*payload.Assignments) > maxAssignmentBatch {
			problem(w, http.StatusUnprocessableEntity, "INVALID_BATCH_SIZE", "assignments must contain between 1 and 50 items")
			return nil, false, false
		}
		return *payload.Assignments, false, true
	}
	legacy := paymentMethodAssignmentRequest{}
	if payload.PaymentMethodCode != nil {
		legacy.PaymentMethodCode = *payload.PaymentMethodCode
	}
	if payload.InstallationID != nil {
		legacy.InstallationID = *payload.InstallationID
	}
	if payload.PaymentMethod != nil {
		legacy.PaymentMethod = *payload.PaymentMethod
	}
	if payload.PaymentMethodType != nil {
		legacy.PaymentMethodType = *payload.PaymentMethodType
	}
	if payload.LegacyLabel != nil {
		legacy.LegacyLabel = *payload.LegacyLabel
	}
	if payload.Version != nil {
		legacy.Version = *payload.Version
	}
	return []paymentMethodAssignmentRequest{legacy}, true, true
}

func (s *Server) preparePaymentMethodAssignment(w http.ResponseWriter, r *http.Request, request paymentMethodAssignmentRequest) (store.UpsertPaymentMethodAssignmentInput, bool) {
	request.InstallationID = strings.TrimSpace(request.InstallationID)
	request.PaymentMethodCode = strings.ToLower(strings.TrimSpace(request.PaymentMethodCode))
	request.PaymentMethod = strings.ToLower(strings.TrimSpace(request.PaymentMethod))
	request.PaymentMethodType = strings.ToLower(strings.TrimSpace(request.PaymentMethodType))
	if request.PaymentMethodCode == "" && request.PaymentMethod == "real_time_payment" && request.PaymentMethodType == "qris" {
		request.PaymentMethodCode = "qris"
	}
	if request.InstallationID == "" || !paymentMethodPattern.MatchString(request.PaymentMethodCode) || request.Version < 0 {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "installation_id, payment_method_code, and non-negative version are required")
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	installation, err := s.store.GetInstallation(r.Context(), tenant(r), request.InstallationID)
	if err != nil {
		s.storeError(w, r, err)
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	if installation.Status != model.InstallationActive {
		problem(w, http.StatusConflict, "INSTALLATION_NOT_ACTIVE", "the installation is not active")
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	capability, err := s.store.GetProviderPaymentMethodCapability(r.Context(), installation.ProviderCode, request.PaymentMethodCode)
	if errors.Is(err, store.ErrNotFound) {
		problem(w, http.StatusUnprocessableEntity, "PAYMENT_METHOD_NOT_SUPPORTED", "the selected gateway does not document support for this master payment method")
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	if err != nil {
		s.internal(w, r, err)
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	if !assignablePaymentMethodCapability(capability.SupportStatus) {
		problem(w, http.StatusConflict, "PAYMENT_METHOD_DISABLED", "the selected gateway payment method is disabled")
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	request.PaymentMethod = capability.ProviderMethod
	request.PaymentMethodType = capability.ProviderMethodType
	if err = s.engine.ValidatePaymentMethodVersion(installation.ProviderCode, installation.ProviderVersion, connector.PaymentMethodMapping{
		PaymentMethodCode:  request.PaymentMethodCode,
		ProviderMethod:     request.PaymentMethod,
		ProviderMethodType: request.PaymentMethodType,
	}); err != nil {
		problem(w, http.StatusUnprocessableEntity, "PAYMENT_METHOD_NOT_SUPPORTED", err.Error())
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	id, ok := s.newID(w, r, "pmo")
	if !ok {
		return store.UpsertPaymentMethodAssignmentInput{}, false
	}
	return store.UpsertPaymentMethodAssignmentInput{
		ID: id, TenantID: tenant(r), Environment: installation.Environment, PaymentMethodCode: request.PaymentMethodCode, PaymentMethod: request.PaymentMethod,
		PaymentMethodType: request.PaymentMethodType, InstallationID: request.InstallationID,
		ExpectedVersion: request.Version, Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	}, true
}

func (s *Server) deactivatePaymentMethodAssignment(w http.ResponseWriter, r *http.Request) {
	var request versionRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Version < 1 {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "version must be a positive integer")
		return
	}
	item, err := s.store.DeactivatePaymentMethodAssignment(r.Context(), tenant(r), chi.URLParam(r, "id"), actor(r), middleware.GetReqID(r.Context()), request.Version)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

type connectorCertificationRequest struct {
	InstallationID    string `json:"installation_id"`
	PaymentMethodCode string `json:"payment_method_code"`
	PaymentID         string `json:"payment_id"`
}

func (s *Server) listConnectorCertifications(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireTenantContext(w, r); !ok {
		return
	}
	environment := optionalMode(r)
	providerCode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider")))
	if len(providerCode) > 64 || (providerCode != "" && !regexp.MustCompile(`^[a-z0-9_-]+$`).MatchString(providerCode)) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER", "provider filter is invalid")
		return
	}
	limit, err := queryInt(r.URL.Query().Get("limit"), 25, 1, 100)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_LIMIT", "limit must be between 1 and 100")
		return
	}
	items, err := s.store.ListConnectorCertificationRuns(r.Context(), tenant(r), environment, providerCode, limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) runConnectorCertification(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireTenantContext(w, r); !ok {
		return
	}
	mode, ok := requireMode(w, r)
	if !ok {
		return
	}
	if mode != model.EnvironmentSandbox {
		problem(w, http.StatusConflict, "SANDBOX_ONLY", "connector certification can only run against a sandbox installation")
		return
	}
	var request connectorCertificationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.InstallationID = strings.TrimSpace(request.InstallationID)
	request.PaymentMethodCode = strings.ToLower(strings.TrimSpace(request.PaymentMethodCode))
	request.PaymentID = strings.TrimSpace(request.PaymentID)
	if !strings.HasPrefix(request.InstallationID, "ins_") || !paymentMethodPattern.MatchString(request.PaymentMethodCode) || (request.PaymentID != "" && !strings.HasPrefix(request.PaymentID, "pay_")) {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "installation_id and payment_method_code are required")
		return
	}
	installation, err := s.store.GetInstallation(r.Context(), tenant(r), request.InstallationID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if installation.Environment != mode || installation.Status != model.InstallationActive {
		problem(w, http.StatusConflict, "INSTALLATION_NOT_ACTIVE", "certification requires an active sandbox installation")
		return
	}
	capability, err := s.store.GetProviderPaymentMethodCapability(r.Context(), installation.ProviderCode, request.PaymentMethodCode)
	if errors.Is(err, store.ErrNotFound) {
		problem(w, http.StatusUnprocessableEntity, "PAYMENT_METHOD_NOT_SUPPORTED", "the provider does not document this payment method")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	runID, ok := s.newID(w, r, "cert")
	if !ok {
		return
	}
	checks := []model.ConnectorCertificationCheck{
		{Code: "installation", Label: "Active sandbox installation", Status: "PASSED", Detail: installation.ID},
		{Code: "catalog_mapping", Label: "Canonical connector mapping", Status: "PASSED", Detail: capability.ProviderMethod + "/" + capability.ProviderMethodType},
	}
	if err = s.engine.ValidatePaymentMethodVersion(installation.ProviderCode, installation.ProviderVersion, connector.PaymentMethodMapping{
		PaymentMethodCode:  capability.PaymentMethodCode,
		ProviderMethod:     capability.ProviderMethod,
		ProviderMethodType: capability.ProviderMethodType,
	}); err != nil {
		checks[1].Status = "FAILED"
		checks[1].Detail = err.Error()
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", "", "The catalog mapping is not executable by the registered connector.", checks)
		return
	}
	manifest, manifestErr := s.engine.ManifestVersion(capability.ProviderCode, installation.ProviderVersion)
	if manifestErr != nil {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "test_profile", Label: "Automated sandbox test profile", Status: "BLOCKED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "BLOCKED", "", "The provider connector is not registered in the running Emisell Payment Engine.", checks)
		return
	}
	profile, profileAvailable := manifest.CertificationProfile(capability.PaymentMethodCode)
	if !profileAvailable || !profile.Automated {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "test_profile", Label: "Automated sandbox test profile", Status: "BLOCKED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "BLOCKED", "", "Automated certification profile is not available for this connector method.", checks)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "test_profile", Label: "Automated sandbox test profile", Status: "PASSED", Detail: profile.Code})
	if request.PaymentID != "" {
		s.resumeConnectorCertification(w, r, runID, request.PaymentID, installation, capability, checks)
		return
	}
	credentials, err := s.loadCredentials(r.Context(), tenant(r), installation.ID)
	if err != nil {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "credential_vault", Label: "Encrypted connector credential", Status: "FAILED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", "", "Credential connector belum tersedia; konfigurasi ulang installation.", checks)
		return
	}
	defer clearCredentials(credentials)
	checks = append(checks, model.ConnectorCertificationCheck{Code: "credential_vault", Label: "Encrypted connector credential", Status: "PASSED"})

	paymentID, ok := s.newID(w, r, "pay")
	if !ok {
		return
	}
	idempotencyKey := "certification-" + runID
	reference := "cert-" + strings.TrimPrefix(runID, "cert_")
	requestHash, err := canonicalHash(map[string]any{"installation_id": installation.ID, "payment_method_code": capability.PaymentMethodCode, "run_id": runID})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	session, created, err := s.store.ReservePayment(r.Context(), store.ReservePaymentInput{
		ID: paymentID, TenantID: tenant(r), InstallationID: installation.ID, ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion,
		Environment: mode, MerchantReference: reference, IdempotencyKey: idempotencyKey, RequestHash: requestHash,
		PaymentMethodCode: capability.PaymentMethodCode, Amount: 1_000_000, Currency: "IDR", ExecutionEngine: "emisell_native",
	})
	if err != nil || !created {
		if err == nil {
			err = store.ErrConflict
		}
		s.internal(w, r, err)
		return
	}
	result, err := s.engine.CreatePayment(r.Context(), connector.PaymentInput{
		ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion, Environment: mode, Credentials: credentials,
		InstallationID: installation.ID, LocalPaymentID: session.ID, MerchantReference: reference,
		IdempotencyKey: idempotencyKey, Amount: 1_000_000, Currency: "IDR",
		PaymentMethodCode: capability.PaymentMethodCode, ChannelCode: capability.ProviderChannelCode,
		PublicWebhookURL: s.providerWebhookURL(installation.ProviderCode, installation.ID),
		Customer:         connector.Customer{Name: "Emisell Certification", Email: "sandbox@example.com", Phone: "+6281234567890"},
		Items:            []connector.Item{{ReferenceID: "certification-item", Type: "DIGITAL_PRODUCT", Name: "Emisell certification item", NetUnitAmount: 1_000_000, Quantity: 1, Category: "software"}},
		ReturnURL:        s.certificationReturnURL(),
		Metadata:         map[string]any{"emisell_tenant_id": tenant(r), "emisell_payment_id": session.ID, "emisell_certification_run_id": runID},
	})
	if err != nil {
		paymentStatus := model.PaymentFailed
		if errors.Is(err, connector.ErrOutcomeUnknown) {
			paymentStatus = model.PaymentUnknown
		}
		_, _ = s.store.FailPayment(r.Context(), tenant(r), session.ID, paymentStatus, "certification.engine.create", safeEngineError(err))
		checks = append(checks, model.ConnectorCertificationCheck{Code: "payment_create", Label: "Sandbox payment create", Status: "FAILED", Detail: safeEngineError(err)})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", session.ID, "Sandbox payment create failed.", checks)
		return
	}
	session, err = s.store.CompletePayment(r.Context(), tenant(r), session.ID, result.ID, result.ConnectorTransactionID, mapPaymentStatus(result.Status), "certification.engine.create", result.NextAction)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "payment_create", Label: "Sandbox payment create", Status: "PASSED", Detail: session.Status})
	if len(result.NextAction) == 0 || string(result.NextAction) == "null" || string(result.NextAction) == "{}" {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "next_action", Label: "Customer next action", Status: "FAILED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", session.ID, "Engine accepted the payment but returned no customer next action.", checks)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "next_action", Label: "Customer next action", Status: "PASSED"})
	synced, syncErr := s.engine.GetPayment(r.Context(), connector.PaymentLookup{
		ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion, Environment: mode, Credentials: credentials, PaymentID: result.ID,
	})
	if syncErr != nil || synced.ID != result.ID {
		detail := "payment identity mismatch"
		if syncErr != nil {
			detail = safeEngineError(syncErr)
		}
		checks = append(checks, model.ConnectorCertificationCheck{Code: "payment_retrieve", Label: "Engine payment retrieval", Status: "FAILED", Detail: detail})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", session.ID, "Sandbox payment could not be retrieved consistently.", checks)
		return
	}
	_, _ = s.store.CompletePayment(r.Context(), tenant(r), session.ID, synced.ID, synced.ConnectorTransactionID, mapPaymentStatus(synced.Status), "certification.engine.sync", synced.NextAction)
	checks = append(checks, model.ConnectorCertificationCheck{Code: "payment_retrieve", Label: "Provider payment retrieval", Status: "PASSED", Detail: synced.Status})
	if err = s.engine.SimulatePayment(r.Context(), connector.PaymentLookup{
		ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion, Environment: mode, Credentials: credentials, PaymentID: result.ID,
	}, session.Amount, session.Currency); err != nil {
		if isManualCertificationAction(err, result.NextAction) {
			detail := "Complete the provider sandbox action, then verify this payment."
			message := "Complete the provider sandbox customer action, then verify the same payment from this certification run."
			if capability.PaymentMethodCode == "ewallet_ovo" {
				detail = "Approve this payment in the OVO sandbox app; the connector returned mobile_authorization without a browser simulator URL."
				message = "OVO sandbox approval is required in the mobile app. Keep this payment and verify it again after approval."
			}
			checks = append(checks, model.ConnectorCertificationCheck{Code: "customer_authorization", Label: "Sandbox customer authorization", Status: "BLOCKED", Detail: detail})
			s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "BLOCKED", session.ID, message, checks)
			return
		}
		checks = append(checks, model.ConnectorCertificationCheck{Code: "sandbox_simulation", Label: "Provider sandbox simulation", Status: "FAILED", Detail: safeEngineError(err)})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", session.ID, "Provider sandbox simulation failed.", checks)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "sandbox_simulation", Label: "Provider sandbox simulation", Status: "PASSED"})
	metadata := map[string]any{}
	_ = json.Unmarshal(installation.CredentialMetadata, &metadata)
	webhookReady, _ := metadata["webhook_ready"].(bool)
	webhookVerified := false
	emisellDelivered := false
	if webhookReady {
		webhookVerified, emisellDelivered, err = s.waitForCertificationEvidence(r.Context(), tenant(r), installation.ProviderCode, session.ID, 12*time.Second)
		if err != nil {
			s.internal(w, r, err)
			return
		}
	}
	if webhookVerified {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "webhook_delivery", Label: "Direct provider webhook received", Status: "PASSED"})
	} else {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "webhook_delivery", Label: "Direct provider webhook received", Status: "BLOCKED", Detail: profile.WebhookSetupHint})
	}
	if emisellDelivered {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "emisell_delivery", Label: "Signed event delivered to Emisell Backend", Status: "PASSED"})
	} else {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "emisell_delivery", Label: "Signed event delivered to Emisell Backend", Status: "BLOCKED", Detail: "Configure and enable the Emisell Backend callback, then verify the same payment again."})
	}
	resultStatus := "PASSED"
	message := manifest.Name + " connector create, retrieve, simulation, provider webhook, and Emisell delivery passed; the method is certified for checkout."
	if !webhookVerified || !emisellDelivered {
		resultStatus = "BLOCKED"
		message = "Payment flow passed, but the complete provider-to-Emisell webhook chain is not yet verified."
	}
	s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, resultStatus, session.ID, message, checks)
}

func (s *Server) certificationReturnURL() string {
	if value := strings.TrimSpace(s.cfg.CertificationReturnURL); value != "" {
		return value
	}
	// Hosted provider flows reject non-HTTPS return URLs. Production requires an
	// explicit value; this development fallback keeps sandbox certification valid.
	return "https://emisell.com"
}

func (s *Server) providerWebhookURL(providerCode, installationID string) string {
	if strings.TrimSpace(s.cfg.PublicBaseURL) == "" {
		return ""
	}
	return strings.TrimRight(s.cfg.PublicBaseURL, "/") + "/webhooks/v1/providers/" +
		url.PathEscape(providerCode) + "/" + url.PathEscape(installationID)
}

func (s *Server) withProviderWebhookURL(installation model.Installation) model.Installation {
	installation.PublicWebhookURL = s.providerWebhookURL(installation.ProviderCode, installation.ID)
	return installation
}

func (s *Server) resumeConnectorCertification(w http.ResponseWriter, r *http.Request, runID, paymentID string, installation model.Installation, capability model.ProviderPaymentMethodCapability, checks []model.ConnectorCertificationCheck) {
	matches, err := s.store.CertificationPaymentMatches(r.Context(), tenant(r), paymentID, installation.ProviderCode, capability.PaymentMethodCode)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !matches {
		problem(w, http.StatusUnprocessableEntity, "CERTIFICATION_PAYMENT_MISMATCH", "payment_id does not belong to this connector certification profile")
		return
	}
	payment, err := s.store.GetPayment(r.Context(), tenant(r), paymentID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if payment.InstallationID != installation.ID || payment.Environment != model.EnvironmentSandbox || payment.EnginePaymentID == "" {
		problem(w, http.StatusConflict, "CERTIFICATION_PAYMENT_NOT_READY", "the certification payment cannot be resumed")
		return
	}
	credentials, err := s.loadCredentials(r.Context(), tenant(r), installation.ID)
	if err != nil {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "credential_vault", Label: "Encrypted connector credential", Status: "FAILED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", payment.ID, "Connector credentials are unavailable.", checks)
		return
	}
	defer clearCredentials(credentials)
	checks = append(checks, model.ConnectorCertificationCheck{Code: "credential_vault", Label: "Encrypted connector credential", Status: "PASSED"})
	synced, err := s.engine.GetPayment(r.Context(), connector.PaymentLookup{ProviderCode: installation.ProviderCode, ProviderVersion: payment.ProviderVersion, Environment: payment.Environment, Credentials: credentials, PaymentID: payment.EnginePaymentID})
	if err != nil {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "payment_retrieve", Label: "Provider payment retrieval", Status: "FAILED", Detail: safeEngineError(err)})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "FAILED", payment.ID, "Provider payment could not be retrieved.", checks)
		return
	}
	payment, err = s.store.CompletePayment(r.Context(), tenant(r), payment.ID, synced.ID, synced.ConnectorTransactionID, mapPaymentStatus(synced.Status), "certification.resume", synced.NextAction)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "payment_retrieve", Label: "Provider payment retrieval", Status: "PASSED", Detail: synced.Status})
	if payment.Status != model.PaymentSucceeded {
		message := "Complete the provider sandbox customer action, then resume this payment again."
		if capability.PaymentMethodCode == "ewallet_ovo" {
			message = "OVO mobile approval is still pending. Approve the original payment in the OVO sandbox app, then check it again."
		}
		checks = append(checks, model.ConnectorCertificationCheck{Code: "customer_authorization", Label: "Sandbox customer authorization", Status: "BLOCKED", Detail: payment.Status})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "BLOCKED", payment.ID, message, checks)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "customer_authorization", Label: "Sandbox customer authorization", Status: "PASSED"})
	webhookVerified, emisellDelivered, err := s.waitForCertificationEvidence(r.Context(), tenant(r), installation.ProviderCode, payment.ID, 7*time.Second)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !webhookVerified {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "webhook_delivery", Label: "Direct provider webhook received", Status: "BLOCKED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "BLOCKED", payment.ID, "Payment succeeded, but its provider webhook has not been processed yet.", checks)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "webhook_delivery", Label: "Direct provider webhook received", Status: "PASSED"})
	if !emisellDelivered {
		checks = append(checks, model.ConnectorCertificationCheck{Code: "emisell_delivery", Label: "Signed event delivered to Emisell Backend", Status: "BLOCKED"})
		s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "BLOCKED", payment.ID, "Provider webhook passed, but the signed event has not been delivered to Emisell Backend.", checks)
		return
	}
	checks = append(checks, model.ConnectorCertificationCheck{Code: "emisell_delivery", Label: "Signed event delivered to Emisell Backend", Status: "PASSED"})
	s.writeConnectorCertification(w, r, http.StatusCreated, runID, installation, capability, "PASSED", payment.ID, "Provider customer authorization, webhook processing, and Emisell delivery passed; the method is certified for checkout.", checks)
}

func (s *Server) waitForCertificationEvidence(ctx context.Context, tenantID, providerCode, paymentID string, wait time.Duration) (bool, bool, error) {
	deadline := time.Now().Add(wait)
	for {
		webhookProcessed, err := s.store.HasProcessedPaymentWebhook(ctx, tenantID, providerCode, paymentID, model.PaymentSucceeded)
		if err != nil {
			return false, false, err
		}
		emisellDelivered := false
		if webhookProcessed {
			emisellDelivered, err = s.store.HasDeliveredPaymentOutbox(ctx, tenantID, paymentID, model.PaymentSucceeded)
			if err != nil {
				return false, false, err
			}
		}
		if (webhookProcessed && emisellDelivered) || !time.Now().Before(deadline) {
			return webhookProcessed, emisellDelivered, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return false, false, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Server) writeConnectorCertification(w http.ResponseWriter, r *http.Request, status int, runID string, installation model.Installation, capability model.ProviderPaymentMethodCapability, resultStatus, paymentID, message string, checks []model.ConnectorCertificationCheck) {
	payload, err := json.Marshal(checks)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	item, err := s.store.CreateConnectorCertificationRun(r.Context(), store.CreateConnectorCertificationRunInput{
		ID: runID, TenantID: tenant(r), InstallationID: installation.ID, ProviderCode: installation.ProviderCode,
		PaymentMethodCode: capability.PaymentMethodCode, Environment: installation.Environment, Status: resultStatus,
		Checks: payload, PaymentID: paymentID, Message: message, Actor: actor(r), RequestID: middleware.GetReqID(r.Context()),
	})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if resultStatus == "PASSED" {
		if err = s.store.CertifyProviderPaymentMethodCapability(r.Context(), tenant(r), installation.ProviderCode, capability.PaymentMethodCode, runID, actor(r), middleware.GetReqID(r.Context())); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	writeData(w, status, item)
}

type paymentRequest struct {
	InstallationID    string             `json:"installation_id"`
	PaymentOptionID   string             `json:"payment_option_id"`
	CheckoutMode      string             `json:"checkout_mode"`
	PaymentMethodCode string             `json:"payment_method_code"`
	MerchantReference string             `json:"merchant_reference"`
	Amount            int64              `json:"amount"`
	Currency          string             `json:"currency"`
	Confirm           *bool              `json:"confirm"`
	CaptureMethod     string             `json:"capture_method"`
	PaymentMethod     string             `json:"payment_method"`
	PaymentMethodType string             `json:"payment_method_type"`
	PaymentMethodData map[string]any     `json:"payment_method_data"`
	ReturnURL         string             `json:"return_url"`
	Description       string             `json:"description"`
	ExpiresAt         string             `json:"expires_at"`
	Customer          connector.Customer `json:"customer"`
	Items             []connector.Item   `json:"items"`
	Metadata          map[string]any     `json:"metadata"`
}

func (s *Server) createPayment(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var request paymentRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.MerchantReference = strings.TrimSpace(request.MerchantReference)
	request.InstallationID = strings.TrimSpace(request.InstallationID)
	request.PaymentOptionID = strings.TrimSpace(request.PaymentOptionID)
	request.CheckoutMode = strings.ToLower(strings.TrimSpace(request.CheckoutMode))
	request.PaymentMethodCode = strings.ToLower(strings.TrimSpace(request.PaymentMethodCode))
	request.PaymentMethod = strings.ToLower(strings.TrimSpace(request.PaymentMethod))
	request.PaymentMethodType = strings.ToLower(strings.TrimSpace(request.PaymentMethodType))
	if request.CheckoutMode == "" {
		if request.PaymentOptionID == "" && request.PaymentMethodCode == "" && request.PaymentMethod == "" && request.PaymentMethodType == "" {
			request.CheckoutMode = connector.CheckoutModeProviderHosted
		} else {
			request.CheckoutMode = connector.CheckoutModeDirect
		}
	}
	if request.CheckoutMode != connector.CheckoutModeProviderHosted && request.CheckoutMode != connector.CheckoutModeDirect {
		problem(w, http.StatusUnprocessableEntity, "INVALID_CHECKOUT_MODE", "checkout_mode must be provider_hosted or direct")
		return
	}
	if request.CheckoutMode == connector.CheckoutModeProviderHosted && (request.InstallationID == "" || request.PaymentOptionID != "" || request.PaymentMethodCode != "" || request.PaymentMethod != "" || request.PaymentMethodType != "" || len(request.PaymentMethodData) > 0 || request.Confirm != nil || strings.TrimSpace(request.CaptureMethod) != "") {
		problem(w, http.StatusUnprocessableEntity, "HOSTED_CHECKOUT_CONFLICT", "provider_hosted checkout requires installation_id and must not select a payment method")
		return
	}
	request.ReturnURL = strings.TrimSpace(request.ReturnURL)
	if request.CheckoutMode == connector.CheckoutModeProviderHosted && (request.ReturnURL == "" || !optionalPublicHTTPSURL(request.ReturnURL)) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_RETURN_URL", "provider_hosted checkout requires a public HTTPS return_url")
		return
	}
	if (request.InstallationID == "" && request.PaymentOptionID == "") || request.MerchantReference == "" || request.Amount <= 0 || len(request.Currency) != 3 {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "payment_option_id or installation_id, merchant_reference, positive amount, and 3-letter currency are required")
		return
	}
	var installation model.Installation
	var err error
	var mode string
	if request.PaymentOptionID != "" {
		assignment, assignmentErr := s.store.GetPaymentMethodAssignment(r.Context(), tenant(r), request.PaymentOptionID)
		if assignmentErr != nil {
			s.storeError(w, r, assignmentErr)
			return
		}
		mode = assignment.Environment
	} else {
		installation, err = s.store.GetInstallation(r.Context(), tenant(r), request.InstallationID)
		if err != nil {
			s.storeError(w, r, err)
			return
		}
		mode = installation.Environment
	}
	hash, err := canonicalHash(request)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if existing, found, lookupErr := s.store.FindPaymentByIdempotency(r.Context(), tenant(r), mode, key, hash); lookupErr != nil {
		s.storeError(w, r, lookupErr)
		return
	} else if found {
		s.recordIntegrationEvidence(r, mode, "idempotency_replay", map[string]any{"payment_id": existing.ID})
		writeData(w, http.StatusOK, existing)
		return
	}
	if request.CheckoutMode == connector.CheckoutModeDirect && request.PaymentOptionID != "" {
		option, optionErr := s.store.GetActivePaymentOption(r.Context(), tenant(r), mode, request.PaymentOptionID)
		if optionErr != nil {
			s.storeError(w, r, optionErr)
			return
		}
		if request.InstallationID != "" && request.InstallationID != option.InstallationID {
			problem(w, http.StatusUnprocessableEntity, "PAYMENT_OPTION_MISMATCH", "payment_option_id does not belong to installation_id")
			return
		}
		if request.PaymentMethod != "" && request.PaymentMethod != option.PaymentMethod {
			problem(w, http.StatusUnprocessableEntity, "PAYMENT_OPTION_MISMATCH", "payment_method does not match payment_option_id")
			return
		}
		if request.PaymentMethodType != "" && request.PaymentMethodType != option.PaymentMethodType {
			problem(w, http.StatusUnprocessableEntity, "PAYMENT_OPTION_MISMATCH", "payment_method_type does not match payment_option_id")
			return
		}
		request.InstallationID = option.InstallationID
		request.PaymentMethodCode = option.PaymentMethodCode
		request.PaymentMethod = option.PaymentMethod
		request.PaymentMethodType = option.PaymentMethodType
	}
	if installation.ID == "" {
		installation, err = s.store.GetInstallation(r.Context(), tenant(r), request.InstallationID)
		if err != nil {
			s.storeError(w, r, err)
			return
		}
	}
	if installation.Environment != mode || installation.Status != model.InstallationActive {
		problem(w, http.StatusConflict, "INSTALLATION_NOT_ACTIVE", "the installation is not active for the selected payment environment")
		return
	}
	var capability model.ProviderPaymentMethodCapability
	if request.CheckoutMode == connector.CheckoutModeDirect {
		if request.PaymentMethodCode == "" {
			request.PaymentMethodCode = paymentMethodCodeFromLegacy(request.PaymentMethod, request.PaymentMethodType)
		}
		capability, err = s.store.GetProviderPaymentMethodCapability(r.Context(), installation.ProviderCode, request.PaymentMethodCode)
		if errors.Is(err, store.ErrNotFound) {
			problem(w, http.StatusUnprocessableEntity, "PAYMENT_METHOD_NOT_SUPPORTED", "the selected provider does not support this payment method")
			return
		}
		if err != nil {
			s.internal(w, r, err)
			return
		}
		if !assignablePaymentMethodCapability(capability.SupportStatus) {
			problem(w, http.StatusConflict, "PAYMENT_METHOD_DISABLED", "the selected gateway payment method is disabled")
			return
		}
		if err = s.engine.ValidatePaymentMethodVersion(installation.ProviderCode, installation.ProviderVersion, connector.PaymentMethodMapping{
			PaymentMethodCode:  capability.PaymentMethodCode,
			ProviderMethod:     capability.ProviderMethod,
			ProviderMethodType: capability.ProviderMethodType,
		}); err != nil {
			problem(w, http.StatusUnprocessableEntity, "PAYMENT_METHOD_NOT_SUPPORTED", err.Error())
			return
		}
		if err = s.engine.ValidatePaymentVersion(installation.ProviderCode, installation.ProviderVersion, connector.PaymentValidation{
			PaymentMethodCode: request.PaymentMethodCode,
			Currency:          request.Currency,
			Amount:            request.Amount,
		}); err != nil {
			problem(w, http.StatusUnprocessableEntity, "INVALID_AMOUNT", err.Error())
			return
		}
	}
	credentials, err := s.loadCredentials(r.Context(), tenant(r), installation.ID)
	if err != nil {
		problem(w, http.StatusConflict, "CONNECTOR_CREDENTIAL_MISSING", "reconfigure the provider installation before creating payments")
		return
	}
	defer clearCredentials(credentials)
	id, ok := s.newID(w, r, "pay")
	if !ok {
		return
	}
	session, created, err := s.store.ReservePayment(r.Context(), store.ReservePaymentInput{ID: id, TenantID: tenant(r), InstallationID: installation.ID, PaymentOptionID: request.PaymentOptionID, PaymentMethodCode: request.PaymentMethodCode, ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion, Environment: mode, MerchantReference: request.MerchantReference, IdempotencyKey: key, RequestHash: hash, Amount: request.Amount, Currency: request.Currency, ExecutionEngine: installation.ExecutionEngine})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !created {
		s.recordIntegrationEvidence(r, mode, "idempotency_replay", map[string]any{"payment_id": session.ID})
		writeData(w, http.StatusOK, session)
		return
	}
	result, err := s.engine.CreatePayment(r.Context(), connector.PaymentInput{
		ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion, Environment: mode, Credentials: credentials,
		InstallationID: installation.ID, LocalPaymentID: session.ID, MerchantReference: request.MerchantReference,
		IdempotencyKey: key, Amount: request.Amount, Currency: request.Currency,
		CheckoutMode: request.CheckoutMode, PaymentMethodCode: request.PaymentMethodCode, ChannelCode: capability.ProviderChannelCode,
		PublicWebhookURL: s.providerWebhookURL(installation.ProviderCode, installation.ID),
		Customer:         request.Customer, Items: request.Items, ReturnURL: request.ReturnURL, Description: request.Description, ExpiresAt: request.ExpiresAt,
		Metadata: mergeMetadata(request.Metadata, map[string]any{"emisell_tenant_id": tenant(r), "emisell_payment_id": session.ID, "emisell_checkout_mode": request.CheckoutMode}),
	})
	if err != nil {
		status := model.PaymentFailed
		if errors.Is(err, connector.ErrOutcomeUnknown) {
			status = model.PaymentUnknown
		}
		session, _ = s.store.FailPayment(r.Context(), tenant(r), session.ID, status, "engine.create", safeEngineError(err))
		s.engineErrorWithData(w, err, session)
		return
	}
	session, err = s.store.CompletePayment(r.Context(), tenant(r), session.ID, result.ID, result.ConnectorTransactionID, mapPaymentStatus(result.Status), "engine.create", result.NextAction)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	response := map[string]any{"payment": session}
	if result.ClientSecret != "" {
		response["client_secret"] = result.ClientSecret
	}
	writeData(w, http.StatusCreated, response)
}

func (s *Server) listPayments(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	environment, ok := optionalEnvironmentQuery(w, r)
	if !ok {
		return
	}
	status := strings.ToUpper(strings.TrimSpace(query.Get("status")))
	if status != "" && !validPaymentStatus(status) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_STATUS", "status is not a supported payment status")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(query.Get("provider")))
	if len(provider) > 64 || (provider != "" && !regexp.MustCompile(`^[a-z0-9_-]+$`).MatchString(provider)) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_PROVIDER", "provider filter is invalid")
		return
	}
	search := strings.TrimSpace(query.Get("q"))
	if len(search) > 128 {
		problem(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "q must not exceed 128 characters")
		return
	}
	limit, err := queryInt(query.Get("limit"), 25, 1, 100)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_LIMIT", "limit must be between 1 and 100")
		return
	}
	offset, err := queryInt(query.Get("offset"), 0, 0, 10000)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_OFFSET", "offset must be between 0 and 10000")
		return
	}
	items, err := s.store.ListPayments(r.Context(), tenant(r), store.PaymentListFilter{Environment: environment, Status: status, Provider: provider, Query: search, Limit: limit, Offset: offset})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) getPayment(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetPayment(r.Context(), tenant(r), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if item.EnginePaymentID != "" && item.ExecutionEngine == "emisell_native" {
		credentials, credentialErr := s.loadCredentials(r.Context(), tenant(r), item.InstallationID)
		if credentialErr == nil {
			defer clearCredentials(credentials)
			result, syncErr := s.engine.GetPayment(r.Context(), connector.PaymentLookup{ProviderCode: item.ProviderCode, ProviderVersion: item.ProviderVersion, Environment: item.Environment, Credentials: credentials, PaymentID: item.EnginePaymentID})
			if syncErr == nil {
				item, _ = s.store.CompletePayment(r.Context(), tenant(r), item.ID, result.ID, result.ConnectorTransactionID, mapPaymentStatus(result.Status), "engine.sync", result.NextAction)
			}
		}
	}
	s.recordIntegrationEvidence(r, item.Environment, "payment_status_read", map[string]any{"payment_id": item.ID})
	writeData(w, http.StatusOK, item)
}

func (s *Server) recordIntegrationEvidence(r *http.Request, environment, code string, details any) {
	if err := s.store.RecordIntegrationEvidence(r.Context(), tenant(r), environment, code, details); err != nil {
		s.log.Warn("record integration evidence", "code", code, "environment", environment, "error", err)
	}
}

func (s *Server) paymentTimeline(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.PaymentTimeline(r.Context(), tenant(r), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

type cancelRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) cancelPayment(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var request cancelRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &request); err != nil {
			return
		}
	}
	item, err := s.store.GetPayment(r.Context(), tenant(r), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if item.EnginePaymentID == "" {
		problem(w, http.StatusConflict, "PAYMENT_NOT_SUBMITTED", "payment has no provider payment ID")
		return
	}
	if item.Status == model.PaymentUnknown {
		problem(w, http.StatusConflict, "PAYMENT_OUTCOME_UNKNOWN", "synchronize the payment before attempting cancellation")
		return
	}
	if item.Status != model.PaymentPending && item.Status != model.PaymentProcessing {
		problem(w, http.StatusConflict, "PAYMENT_NOT_CANCELLABLE", "only pending or processing payments can be cancelled")
		return
	}
	if item.ExecutionEngine != "emisell_native" {
		problem(w, http.StatusConflict, "LEGACY_PAYMENT_READ_ONLY", "payments from the previous runtime are read-only after migration")
		return
	}
	credentials, err := s.loadCredentials(r.Context(), tenant(r), item.InstallationID)
	if err != nil {
		problem(w, http.StatusConflict, "CONNECTOR_CREDENTIAL_MISSING", "provider credential is not available")
		return
	}
	defer clearCredentials(credentials)
	result, err := s.engine.CancelPayment(r.Context(), connector.PaymentLookup{ProviderCode: item.ProviderCode, ProviderVersion: item.ProviderVersion, Environment: item.Environment, Credentials: credentials, PaymentID: item.EnginePaymentID}, key, request.Reason)
	if err != nil {
		if errors.Is(err, connector.ErrNotSupported) {
			problem(w, http.StatusUnprocessableEntity, "CANCEL_NOT_SUPPORTED", "the connector does not support cancellation for this payment method")
			return
		}
		s.engineError(w, err)
		return
	}
	item, err = s.store.CompletePayment(r.Context(), tenant(r), item.ID, result.ID, result.ConnectorTransactionID, mapPaymentStatus(result.Status), "operator.cancel", result.NextAction)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

type refundRequest struct {
	PaymentID string         `json:"payment_id"`
	Amount    int64          `json:"amount"`
	Reason    string         `json:"reason"`
	Metadata  map[string]any `json:"metadata"`
}

type refundPolicy struct {
	Supported              bool   `json:"supported"`
	Partial                bool   `json:"partial"`
	MultiplePartial        bool   `json:"multiple_partial"`
	ReturnToOriginalSource bool   `json:"return_to_original_source"`
	Confirmation           string `json:"confirmation"`
	WindowDays             int    `json:"window_days"`
}

func refundPolicyFromMetadata(metadata json.RawMessage) (refundPolicy, error) {
	var envelope struct {
		Refund *refundPolicy `json:"refund"`
	}
	if err := json.Unmarshal(metadata, &envelope); err != nil {
		return refundPolicy{}, err
	}
	if envelope.Refund == nil {
		return refundPolicy{}, nil
	}
	envelope.Refund.Confirmation = strings.ToLower(strings.TrimSpace(envelope.Refund.Confirmation))
	return *envelope.Refund, nil
}

func normalizeRefundReason(value string) (string, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	switch normalized {
	case "REQUESTED_BY_CUSTOMER", "CUSTOMER_REQUEST":
		return "REQUESTED_BY_CUSTOMER", true
	case "CANCELLATION", "CANCELLED", "ORDER_CANCELLED":
		return "CANCELLATION", true
	case "DUPLICATE", "FRAUDULENT", "OTHERS":
		return normalized, true
	case "OTHER":
		return "OTHERS", true
	default:
		return "", false
	}
}

func (s *Server) createRefund(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var request refundRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.PaymentID == "" || request.Amount <= 0 {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "payment_id and positive amount are required")
		return
	}
	normalizedReason, validReason := normalizeRefundReason(request.Reason)
	if !validReason {
		problem(w, http.StatusUnprocessableEntity, "INVALID_REFUND_REASON", "reason must be requested_by_customer, cancellation, duplicate, fraudulent, or others")
		return
	}
	request.Reason = normalizedReason
	payment, err := s.store.GetPayment(r.Context(), tenant(r), request.PaymentID)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if payment.Status != model.PaymentSucceeded || payment.EnginePaymentID == "" || request.Amount > payment.Amount {
		problem(w, http.StatusConflict, "PAYMENT_NOT_REFUNDABLE", "payment is not refundable or amount exceeds the payment")
		return
	}
	if payment.ExecutionEngine != "emisell_native" {
		problem(w, http.StatusConflict, "LEGACY_PAYMENT_READ_ONLY", "payments from the previous runtime cannot be refunded automatically")
		return
	}
	if payment.PaymentMethodCode == "" {
		problem(w, http.StatusUnprocessableEntity, "REFUND_POLICY_UNAVAILABLE", "the original payment method is not recorded; automatic refund is fail-closed")
		return
	}
	capability, err := s.store.GetProviderPaymentMethodCapability(r.Context(), payment.ProviderCode, payment.PaymentMethodCode)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	policy, err := refundPolicyFromMetadata(capability.Metadata)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !policy.Supported || !policy.ReturnToOriginalSource {
		problem(w, http.StatusUnprocessableEntity, "REFUND_NOT_SUPPORTED", "the original payment channel does not have a certified return-to-source refund policy")
		return
	}
	if !policy.Partial && request.Amount != payment.Amount {
		problem(w, http.StatusUnprocessableEntity, "PARTIAL_REFUND_NOT_SUPPORTED", "the original payment channel only supports a full refund")
		return
	}
	refundSupported, err := s.engine.SupportsVersion(payment.ProviderCode, payment.ProviderVersion, connector.OperationCreateRefund)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !refundSupported {
		problem(w, http.StatusUnprocessableEntity, "REFUND_NOT_SUPPORTED", "refund is not supported by the installed connector version")
		return
	}
	hash, _ := canonicalHash(request)
	id, ok := s.newID(w, r, "ref")
	if !ok {
		return
	}
	refund, created, err := s.store.ReserveRefund(r.Context(), store.ReserveRefundInput{
		ID: id, TenantID: tenant(r), PaymentID: payment.ID, PaymentMethodCode: payment.PaymentMethodCode,
		IdempotencyKey: key, RequestHash: hash, Amount: request.Amount, Currency: payment.Currency,
		Reason: request.Reason, RequestedBy: actor(r), RequestID: middleware.GetReqID(r.Context()),
		ExecutionEngine: payment.ExecutionEngine, AllowPartial: policy.Partial, AllowMultiple: policy.MultiplePartial,
	})
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if !created && refund.Status != "CREATED" {
		writeData(w, http.StatusOK, refund)
		return
	}
	// A previous attempt can be stranded at CREATED if the provider result
	// could not be persisted. Re-dispatching the same logical request is safe:
	// the exact provider idempotency key is reused and no second refund may be
	// reserved transactionally.
	credentials, err := s.loadCredentials(r.Context(), tenant(r), payment.InstallationID)
	if err != nil {
		_, _ = s.store.FailRefund(r.Context(), tenant(r), refund.ID, "FAILED", "provider credential is not available")
		problem(w, http.StatusConflict, "CONNECTOR_CREDENTIAL_MISSING", "provider credential is not available")
		return
	}
	defer clearCredentials(credentials)
	result, err := s.engine.CreateRefund(r.Context(), connector.RefundInput{
		ProviderCode: payment.ProviderCode, ProviderVersion: payment.ProviderVersion, Environment: payment.Environment, Credentials: credentials,
		PaymentID: payment.EnginePaymentID, IdempotencyKey: key, Amount: request.Amount, Currency: payment.Currency,
		Reason: request.Reason, Metadata: mergeMetadata(request.Metadata, map[string]any{"emisell_tenant_id": tenant(r), "emisell_refund_id": refund.ID}),
	})
	if err != nil {
		status := "FAILED"
		if errors.Is(err, connector.ErrOutcomeUnknown) {
			status = "UNKNOWN"
		}
		refund, _ = s.store.FailRefund(r.Context(), tenant(r), refund.ID, status, safeEngineError(err))
		s.engineErrorWithData(w, err, refund)
		return
	}
	refund, err = s.store.CompleteRefund(r.Context(), tenant(r), refund.ID, result.ID, mapRefundStatus(result.Status))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusCreated, refund)
}
func (s *Server) getRefund(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetRefund(r.Context(), tenant(r), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if item.EngineRefundID != "" && item.ExecutionEngine == "emisell_native" {
		payment, paymentErr := s.store.GetPayment(r.Context(), tenant(r), item.PaymentID)
		if paymentErr == nil {
			lookupSupported, supportErr := s.engine.SupportsVersion(payment.ProviderCode, payment.ProviderVersion, connector.OperationGetRefund)
			if supportErr == nil && lookupSupported {
				credentials, credentialErr := s.loadCredentials(r.Context(), tenant(r), payment.InstallationID)
				if credentialErr == nil {
					defer clearCredentials(credentials)
					result, syncErr := s.engine.GetRefund(r.Context(), connector.RefundLookup{ProviderCode: payment.ProviderCode, ProviderVersion: payment.ProviderVersion, Environment: payment.Environment, Credentials: credentials, RefundID: item.EngineRefundID})
					if syncErr == nil {
						item, _ = s.store.CompleteRefund(r.Context(), tenant(r), item.ID, result.ID, mapRefundStatus(result.Status))
					}
				}
			}
		}
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listWebhookInbox(w http.ResponseWriter, r *http.Request) {
	filter, ok := webhookListFilter(w, r, validWebhookInboxStatus)
	if !ok {
		return
	}
	items, err := s.store.ListWebhookInbox(r.Context(), tenant(r), filter)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

func (s *Server) listWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	filter, ok := webhookListFilter(w, r, validWebhookDeliveryStatus)
	if !ok {
		return
	}
	items, err := s.store.ListWebhookDeliveries(r.Context(), tenant(r), filter)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

type replayWebhookRequest struct {
	ExpectedReplayCount int `json:"expected_replay_count"`
}

func (s *Server) replayWebhookDelivery(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var request replayWebhookRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.ExpectedReplayCount < 0 {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "expected_replay_count must be zero or greater")
		return
	}
	item, err := s.store.ReplayWebhookDelivery(r.Context(), tenant(r), chi.URLParam(r, "id"), actor(r), middleware.GetReqID(r.Context()), key, request.ExpectedReplayCount)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, item)
}

func (s *Server) listReconciliationCases(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	kind := strings.ToUpper(strings.TrimSpace(query.Get("kind")))
	if kind != "" && !validReconciliationKind(kind) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_KIND", "kind is not a supported reconciliation case type")
		return
	}
	search := strings.TrimSpace(query.Get("q"))
	if len(search) > 128 {
		problem(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "q must not exceed 128 characters")
		return
	}
	limit, err := queryInt(query.Get("limit"), 25, 1, 100)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_LIMIT", "limit must be between 1 and 100")
		return
	}
	offset, err := queryInt(query.Get("offset"), 0, 0, 10000)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_OFFSET", "offset must be between 0 and 10000")
		return
	}
	items, err := s.store.ListReconciliationCases(r.Context(), tenant(r), store.ReconciliationListFilter{Kind: kind, Query: search, Limit: limit, Offset: offset})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeData(w, http.StatusOK, items)
}

type resolvePaymentReconciliationRequest struct {
	ExpectedReconciliationCount int `json:"expected_reconciliation_count"`
}

func (s *Server) resolvePaymentReconciliation(w http.ResponseWriter, r *http.Request) {
	key, ok := requireIdempotency(w, r)
	if !ok {
		return
	}
	var request resolvePaymentReconciliationRequest
	if err := decodeJSON(w, r, &request); err != nil {
		return
	}
	if request.ExpectedReconciliationCount < 0 {
		problem(w, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "expected_reconciliation_count must be zero or greater")
		return
	}
	payment, err := s.store.GetPayment(r.Context(), tenant(r), chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	if payment.LastReconciliationKey == key {
		writeData(w, http.StatusOK, payment)
		return
	}
	if payment.Status != model.PaymentUnknown || payment.EnginePaymentID == "" {
		problem(w, http.StatusConflict, "INVALID_STATE", "only UNKNOWN payments with a provider payment ID can be reconciled")
		return
	}
	if payment.ExecutionEngine != "emisell_native" {
		problem(w, http.StatusConflict, "LEGACY_PAYMENT_READ_ONLY", "payments from the previous runtime must be reconciled from archived provider evidence")
		return
	}
	credentials, err := s.loadCredentials(r.Context(), tenant(r), payment.InstallationID)
	if err != nil {
		problem(w, http.StatusConflict, "CONNECTOR_CREDENTIAL_MISSING", "provider credential is not available")
		return
	}
	defer clearCredentials(credentials)
	result, err := s.engine.GetPayment(r.Context(), connector.PaymentLookup{ProviderCode: payment.ProviderCode, ProviderVersion: payment.ProviderVersion, Environment: payment.Environment, Credentials: credentials, PaymentID: payment.EnginePaymentID})
	if err != nil {
		s.engineError(w, err)
		return
	}
	engineID := result.ID
	if engineID == "" {
		engineID = payment.EnginePaymentID
	}
	connectorID := result.ConnectorTransactionID
	if connectorID == "" {
		connectorID = payment.ConnectorTxnID
	}
	payment, err = s.store.ReconcilePayment(r.Context(), tenant(r), payment.ID, actor(r), middleware.GetReqID(r.Context()), key, engineID, connectorID, mapPaymentStatus(result.Status), result.NextAction, request.ExpectedReconciliationCount)
	if err != nil {
		s.storeError(w, r, err)
		return
	}
	writeData(w, http.StatusOK, payment)
}

func (s *Server) providerWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil || len(body) > maxRequestBody {
		problem(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "webhook payload exceeds 1 MiB")
		return
	}
	providerCode := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	installationID := strings.TrimSpace(chi.URLParam(r, "installationID"))
	if !regexp.MustCompile(`^[a-z0-9_-]{2,48}$`).MatchString(providerCode) || !strings.HasPrefix(installationID, "ins_") {
		problem(w, http.StatusNotFound, "NOT_FOUND", "webhook endpoint not found")
		return
	}
	tenantID, encryptedCredentials, err := s.store.GetProviderCredentialsByInstallationID(r.Context(), installationID)
	if err != nil {
		problem(w, http.StatusNotFound, "NOT_FOUND", "webhook endpoint not found")
		return
	}
	installation, err := s.store.GetInstallation(r.Context(), tenantID, installationID)
	if err != nil || installation.ProviderCode != providerCode {
		problem(w, http.StatusNotFound, "NOT_FOUND", "webhook endpoint not found")
		return
	}
	plaintext, err := s.cipher.Decrypt(encryptedCredentials, []byte("provider-credential:"+installationID))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer clear(plaintext)
	credentials := map[string]string{}
	if err = json.Unmarshal(plaintext, &credentials); err != nil {
		s.internal(w, r, err)
		return
	}
	defer clearCredentials(credentials)
	event, err := s.engine.HandleWebhook(r.Context(), connector.WebhookInput{ProviderCode: providerCode, ProviderVersion: installation.ProviderVersion, Credentials: credentials, Headers: r.Header.Clone(), Body: body})
	if err != nil {
		s.metrics.RecordProviderWebhook("invalid")
		var apiErr *connector.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			problem(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "invalid provider webhook signature")
			return
		}
		problem(w, http.StatusBadRequest, "INVALID_WEBHOOK", "provider webhook payload is invalid")
		return
	}
	canonicalStatus := mapPaymentStatus(event.Status)
	if event.RefundID != "" {
		canonicalStatus = mapRefundStatus(event.Status)
	}
	sum := sha256.Sum256(body)
	payloadCiphertext, err := s.cipher.Encrypt(body, []byte(providerCode+":"+event.ID))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	id, ok := s.newID(w, r, "wh")
	if !ok {
		return
	}
	accepted, err := s.store.ProcessWebhook(r.Context(), store.WebhookInput{ID: id, Source: providerCode, ExternalEventID: event.ID, EventType: event.Type, EnginePaymentID: event.PaymentID, EngineRefundID: event.RefundID, Status: canonicalStatus, PayloadHash: sum[:], PayloadCiphertext: payloadCiphertext})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if accepted {
		s.metrics.RecordProviderWebhook("accepted")
	} else {
		s.metrics.RecordProviderWebhook("duplicate")
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": accepted})
}

func (s *Server) authenticateService(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := bearerToken(r.Header.Get("Authorization"))
		valid := value != "" && s.cfg.ServiceAPIKey != "" && constantEqual(value, s.cfg.ServiceAPIKey)
		if !valid && value != "" && s.serviceKeys != nil {
			var err error
			valid, err = s.serviceKeys.Authenticate(r.Context(), value)
			if err != nil {
				s.log.Error("authenticate service API key", "request_id", middleware.GetReqID(r.Context()), "error", err)
				problem(w, http.StatusServiceUnavailable, "AUTHENTICATION_UNAVAILABLE", "service authentication is temporarily unavailable")
				return
			}
		}
		if !valid {
			problem(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid service credential")
			return
		}
		t := strings.TrimSpace(r.Header.Get("X-Emisell-Merchant-ID"))
		if !tenantPattern.MatchString(t) {
			problem(w, http.StatusBadRequest, "INVALID_TENANT", "X-Emisell-Merchant-ID is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) protectServiceTraffic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.rateLimiter != nil && s.rateLimiter.Enabled() {
			decision := s.rateLimiter.Allow(tenant(r))
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
			w.Header().Set("X-Emisell-RateLimit-Scope", "replica-merchant")
			if !decision.Allowed {
				retrySeconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				problem(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "merchant request rate exceeded the API replica limit")
				return
			}
		}
		if s.inFlight != nil {
			select {
			case s.inFlight <- struct{}{}:
				defer func() { <-s.inFlight }()
			default:
				w.Header().Set("Retry-After", "1")
				problem(w, http.StatusServiceUnavailable, "API_BUSY", "API replica is at its concurrent request limit")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) protectAdminTraffic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.adminRateLimiter != nil && s.adminRateLimiter.Enabled() {
			decision := s.adminRateLimiter.Allow(adminRateLimitKey(r))
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
			w.Header().Set("X-Emisell-RateLimit-Scope", "replica-admin-ip")
			if !decision.Allowed {
				retrySeconds := int((decision.RetryAfter + time.Second - 1) / time.Second)
				w.Header().Set("Retry-After", strconv.Itoa(retrySeconds))
				problem(w, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", "admin request rate exceeded the API replica limit")
				return
			}
		}
		if s.inFlight != nil {
			select {
			case s.inFlight <- struct{}{}:
				defer func() { <-s.inFlight }()
			default:
				w.Header().Set("Retry-After", "1")
				problem(w, http.StatusServiceUnavailable, "API_BUSY", "API replica is at its concurrent request limit")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func adminRateLimitKey(r *http.Request) string {
	value := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if value == "" {
		value = "unknown"
	}
	return "admin:" + value
}

func (s *Server) requestDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIRequestTimeout <= 0 {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.APIRequestTimeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func (s *Server) authenticateAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := strings.TrimSpace(r.Header.Get("X-Admin-API-Key"))
		if s.cfg.AdminAPIKey == "" || !constantEqual(value, s.cfg.AdminAPIKey) {
			problem(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid admin credential")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) responseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
		w.Header().Set("Cache-Control", "no-store")
		addVary(w.Header(), "Authorization")
		addVary(w.Header(), "X-Admin-API-Key")
		addVary(w.Header(), "X-Emisell-Merchant-ID")
		addVary(w.Header(), "X-Emisell-Execution-Mode")
		next.ServeHTTP(w, r)
	})
}

func addVary(header http.Header, value string) {
	for _, current := range header.Values("Vary") {
		for _, item := range strings.Split(current, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		s.metrics.RequestStarted()
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			panicValue := recover()
			status := wrapped.Status()
			if panicValue != nil {
				status = http.StatusInternalServerError
			} else if status == 0 {
				status = http.StatusOK
			}
			elapsed := time.Since(started)
			s.metrics.RequestFinished(status, elapsed)
			s.log.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "request_id", middleware.GetReqID(r.Context()), "duration_ms", elapsed.Milliseconds())
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func (s *Server) storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		problem(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case errors.Is(err, store.ErrIdempotencyConflict):
		problem(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error())
	case errors.Is(err, store.ErrConflict):
		problem(w, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, store.ErrVersionConflict):
		problem(w, http.StatusConflict, "VERSION_CONFLICT", err.Error())
	case errors.Is(err, store.ErrInvalidState):
		problem(w, http.StatusConflict, "INVALID_STATE", err.Error())
	default:
		s.internal(w, r, err)
	}
}
func (s *Server) installationError(w http.ResponseWriter, r *http.Request, err error) {
	var domainErr *installationservice.Error
	if errors.As(err, &domainErr) {
		status := http.StatusUnprocessableEntity
		switch domainErr.Code {
		case installationservice.CodeProviderVersionNotRunning,
			installationservice.CodeInvalidState,
			installationservice.CodeRefundLiabilityOpen:
			status = http.StatusConflict
		}
		problem(w, status, domainErr.Code, domainErr.Message)
		return
	}
	var engineErr *installationservice.EngineError
	if errors.As(err, &engineErr) {
		s.engineError(w, engineErr.Cause)
		return
	}
	s.storeError(w, r, err)
}
func (s *Server) engineError(w http.ResponseWriter, err error) { s.engineErrorWithData(w, err, nil) }
func (s *Server) engineErrorWithData(w http.ResponseWriter, err error, data any) {
	status := http.StatusBadGateway
	code := "ENGINE_ERROR"
	message := "provider connector rejected the request"
	if errors.Is(err, connector.ErrOutcomeUnknown) {
		s.metrics.RecordConnectorOutcome("unknown")
		status = http.StatusAccepted
		code = "OUTCOME_UNKNOWN"
		message = "provider outcome is unknown; query the same resource before retrying"
	} else if errors.Is(err, connector.ErrNotSupported) {
		s.metrics.RecordConnectorOutcome("not_supported")
		status = http.StatusUnprocessableEntity
		code = "OPERATION_NOT_SUPPORTED"
		message = "the selected connector does not support this operation"
	} else if errors.Is(err, connector.ErrInvalidCredential) {
		s.metrics.RecordConnectorOutcome("rejected")
		status = http.StatusUnprocessableEntity
		code = "INVALID_PROVIDER_CREDENTIAL"
		message = "provider credential is invalid or its mode cannot be detected"
	} else {
		var apiErr *connector.APIError
		if errors.As(err, &apiErr) && apiErr.Status >= 400 && apiErr.Status < 500 {
			s.metrics.RecordConnectorOutcome("rejected")
			status = http.StatusUnprocessableEntity
			code = apiErr.Code
			message = "provider rejected the request"
		}
	}
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message}, "data": data})
}
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(r.Context().Err(), context.DeadlineExceeded) {
		problem(w, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "request exceeded the Payment Proxy processing deadline")
		return
	}
	s.log.Error("request failed", "request_id", middleware.GetReqID(r.Context()), "error", err)
	problem(w, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}

func (s *Server) newID(w http.ResponseWriter, r *http.Request, prefix string) (string, bool) {
	id, err := ids.New(prefix)
	if err != nil {
		s.internal(w, r, err)
		return "", false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		problem(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		problem(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain one JSON object")
		return errors.New("multiple JSON values")
	}
	return nil
}
func writeData(w http.ResponseWriter, status int, data any) {
	writeJSON(w, status, map[string]any{"data": data})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func tenant(r *http.Request) string { return strings.TrimSpace(r.Header.Get("X-Emisell-Merchant-ID")) }

func requireTenantContext(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := tenant(r)
	if !tenantPattern.MatchString(value) {
		problem(w, http.StatusBadRequest, "INVALID_TENANT", "X-Emisell-Merchant-ID is required for the internal verification tenant")
		return "", false
	}
	return value, true
}
func actor(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/api/v1/admin/") {
		return "payment-proxy-admin"
	}
	return "emisell-backend"
}
func optionalMode(r *http.Request) string {
	value := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Emisell-Execution-Mode")))
	if validMode(value) {
		return value
	}
	return ""
}
func requireMode(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := optionalMode(r)
	if value == "" {
		problem(w, http.StatusBadRequest, "INVALID_EXECUTION_MODE", "X-Emisell-Execution-Mode must be sandbox or live")
		return "", false
	}
	return value, true
}

func optionalEnvironmentQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("environment")))
	if value == "" {
		return "", true
	}
	if !validMode(value) {
		problem(w, http.StatusBadRequest, "INVALID_ENVIRONMENT", "environment query must be sandbox or live")
		return "", false
	}
	return value, true
}

func requireEnvironmentQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	value, ok := optionalEnvironmentQuery(w, r)
	if !ok {
		return "", false
	}
	if value == "" {
		problem(w, http.StatusBadRequest, "INVALID_ENVIRONMENT", "environment query must be sandbox or live")
		return "", false
	}
	return value, true
}
func validMode(value string) bool {
	return value == model.EnvironmentLive || value == model.EnvironmentSandbox
}

func validPaymentStatus(value string) bool {
	switch value {
	case model.PaymentCreated, model.PaymentProcessing, model.PaymentPending, model.PaymentSucceeded, model.PaymentFailed, model.PaymentCancelled, model.PaymentExpired, model.PaymentUnknown:
		return true
	default:
		return false
	}
}
func validWebhookInboxStatus(value string) bool {
	switch value {
	case "RECEIVED", "PROCESSED", "IGNORED", "FAILED":
		return true
	default:
		return false
	}
}
func validWebhookDeliveryStatus(value string) bool {
	switch value {
	case "PENDING", "PROCESSING", "DELIVERED", "DEAD":
		return true
	default:
		return false
	}
}
func validReconciliationKind(value string) bool {
	switch value {
	case "PAYMENT_UNKNOWN", "REFUND_UNKNOWN", "DELIVERY_DEAD", "WEBHOOK_FAILED", "INSTALLATION_ERROR":
		return true
	default:
		return false
	}
}
func webhookListFilter(w http.ResponseWriter, r *http.Request, validStatus func(string) bool) (store.WebhookListFilter, bool) {
	query := r.URL.Query()
	status := strings.ToUpper(strings.TrimSpace(query.Get("status")))
	if status != "" && !validStatus(status) {
		problem(w, http.StatusUnprocessableEntity, "INVALID_STATUS", "status is not supported for this webhook view")
		return store.WebhookListFilter{}, false
	}
	search := strings.TrimSpace(query.Get("q"))
	if len(search) > 128 {
		problem(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "q must not exceed 128 characters")
		return store.WebhookListFilter{}, false
	}
	limit, err := queryInt(query.Get("limit"), 25, 1, 100)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_LIMIT", "limit must be between 1 and 100")
		return store.WebhookListFilter{}, false
	}
	offset, err := queryInt(query.Get("offset"), 0, 0, 10000)
	if err != nil {
		problem(w, http.StatusUnprocessableEntity, "INVALID_OFFSET", "offset must be between 0 and 10000")
		return store.WebhookListFilter{}, false
	}
	return store.WebhookListFilter{Status: status, Query: search, Limit: limit, Offset: offset}, true
}
func queryInt(value string, fallback, min, max int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < min || number > max {
		return 0, errors.New("query number is out of range")
	}
	return number, nil
}
func catalogSearchQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(search) > 128 {
		problem(w, http.StatusUnprocessableEntity, "INVALID_QUERY", "q must not exceed 128 characters")
		return "", false
	}
	return search, true
}
func requireIdempotency(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 8 || len(key) > 128 {
		problem(w, http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 8 to 128 characters")
		return "", false
	}
	return key, true
}
func constantEqual(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }
func canonicalHash(value any) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(payload)
	return sum[:], nil
}
func clearCredentials(values map[string]string) {
	installationservice.ClearCredentials(values)
}
func validateAndMaskCredentials(schema json.RawMessage, credentials map[string]string) ([]byte, error) {
	return installationservice.ValidateAndMaskCredentials(schema, credentials)
}

func validateCredentialFieldNames(schema json.RawMessage, names []string) error {
	return installationservice.ValidateCredentialFieldNames(schema, names)
}

func (s *Server) loadCredentials(ctx context.Context, tenantID, installationID string) (map[string]string, error) {
	ciphertext, err := s.store.GetProviderCredentials(ctx, tenantID, installationID)
	if err != nil {
		return nil, err
	}
	plaintext, err := s.cipher.Decrypt(ciphertext, []byte("provider-credential:"+installationID))
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	credentials := map[string]string{}
	if err = json.Unmarshal(plaintext, &credentials); err != nil {
		return nil, err
	}
	return credentials, nil
}

func mergeMetadata(input, required map[string]any) map[string]any {
	result := map[string]any{}
	for k, v := range input {
		if !strings.HasPrefix(strings.ToLower(k), "emisell_") {
			result[k] = v
		}
	}
	for k, v := range required {
		result[k] = v
	}
	return result
}
func safeEngineError(err error) string {
	var apiErr *connector.APIError
	if errors.As(err, &apiErr) {
		detail := fmt.Sprintf("%s HTTP %d code %s", strings.ToUpper(apiErr.Provider), apiErr.Status, apiErr.Code)
		message := strings.Join(strings.Fields(apiErr.Message), " ")
		if len(message) > 180 {
			message = message[:180]
		}
		if message != "" {
			detail += ": " + message
		}
		return detail
	}
	if errors.Is(err, connector.ErrOutcomeUnknown) {
		return "provider outcome unknown"
	}
	if errors.Is(err, connector.ErrNotSupported) {
		return "connector operation is not supported"
	}
	return "provider request failed"
}

func isManualCertificationAction(err error, nextAction json.RawMessage) bool {
	var apiErr *connector.APIError
	providerCannotSimulate := errors.Is(err, connector.ErrNotSupported) || (errors.As(err, &apiErr) && apiErr.Code == "PAYMENT_METHOD_NOT_SUPPORTED")
	if !providerCannotSimulate {
		return false
	}
	var action struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(nextAction, &action) != nil {
		return false
	}
	switch action.Type {
	case "redirect", "mobile_authorization", "qr_code_information", "virtual_account_information", "provider_actions":
		return true
	default:
		return false
	}
}

func mapPaymentStatus(value string) string {
	switch strings.ToLower(value) {
	case "succeeded", "charged", "captured", "completed":
		return model.PaymentSucceeded
	case "failed", "authentication_failed", "authorization_failed":
		return model.PaymentFailed
	case "cancelled", "canceled", "voided":
		return model.PaymentCancelled
	case "expired":
		return model.PaymentExpired
	case "requires_payment_method", "requires_confirmation":
		return model.PaymentCreated
	case "processing", "requires_capture":
		return model.PaymentProcessing
	case "requires_customer_action", "requires_action", "pending", "active":
		return model.PaymentPending
	default:
		return model.PaymentUnknown
	}
}
func mapRefundStatus(value string) string {
	switch strings.ToLower(value) {
	case "success", "succeeded":
		return "SUCCEEDED"
	case "failure", "failed":
		return "FAILED"
	case "pending", "manual_review":
		return "PENDING"
	case "created":
		return "CREATED"
	default:
		return "UNKNOWN"
	}
}
func paymentMethodCodeFromLegacy(paymentMethod, paymentMethodType string) string {
	if (paymentMethod == "qr_code" || paymentMethod == "real_time_payment") && paymentMethodType == "qris" {
		return "qris"
	}
	if paymentMethod == "bank_transfer" && (paymentMethodType == "bca" || paymentMethodType == "bca_bank_transfer") {
		return "va_bca"
	}
	return ""
}

func assignablePaymentMethodCapability(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case model.PaymentMethodSupportDocumented, model.PaymentMethodSupportCertified:
		return true
	default:
		return false
	}
}
