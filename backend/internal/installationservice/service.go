package installationservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
	"github.com/emisell/api-payment-proxy/internal/ids"
	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/emisell/api-payment-proxy/internal/store"
)

const (
	CodeValidationError           = "VALIDATION_ERROR"
	CodeNoCredentialChanges       = "NO_CREDENTIAL_CHANGES"
	CodeInvalidCredentials        = "INVALID_CREDENTIALS"
	CodeProviderVersionNotRunning = "PROVIDER_VERSION_NOT_RUNNING"
	CodeCredentialModeUnknown     = "CREDENTIAL_MODE_UNKNOWN"
	CodeCredentialModeMismatch    = "CREDENTIAL_MODE_MISMATCH"
	CodeInvalidState              = "INVALID_STATE"
	CodeRefundLiabilityOpen       = "REFUND_LIABILITY_OPEN"
)

type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

type EngineError struct{ Cause error }

func (e *EngineError) Error() string { return e.Cause.Error() }
func (e *EngineError) Unwrap() error { return e.Cause }

type Repository interface {
	GetProvider(context.Context, string) (model.Provider, error)
	GetReleasedProviderVersion(context.Context, string) (string, error)
	CreateInstallation(context.Context, store.CreateInstallationInput) (model.Installation, error)
	GetInstallation(context.Context, string, string) (model.Installation, error)
	BeginCredentialConfig(context.Context, string, string, string, string) (model.Installation, error)
	GetProviderCredentials(context.Context, string, string) ([]byte, error)
	SaveProviderCredentials(context.Context, string, string, []byte) error
	CompleteInstallationVerification(context.Context, store.CompleteInstallationVerificationInput) (model.Installation, error)
	FailInstallationVerification(context.Context, store.FailInstallationVerificationInput) error
	FailInstallation(context.Context, string, string, string, string) error
	TransitionInstallation(context.Context, string, string, string, string, string, int64) (model.Installation, error)
	UpgradeInstallation(context.Context, string, string, string, string, string, int64) (model.Installation, error)
	HasOpenRefundLiability(context.Context, string, string) (bool, error)
	DeleteProviderCredentials(context.Context, string, string) error
	MarkUninstalled(context.Context, string, string, string, string) (model.Installation, error)
}

type Engine interface {
	ManifestVersion(string, string) (connector.Manifest, error)
	VerifyInstallation(context.Context, connector.InstallationInput) (connector.InstallationResult, error)
	DisableInstallation(context.Context, connector.InstallationInput) error
}

type Cipher interface {
	Encrypt([]byte, []byte) ([]byte, error)
	Decrypt([]byte, []byte) ([]byte, error)
}

type Service struct {
	repository    Repository
	engine        Engine
	cipher        Cipher
	publicBaseURL string
	now           func() time.Time
	newID         func(string) (string, error)
}

func New(repository Repository, engine Engine, cipher Cipher, publicBaseURL string) *Service {
	return &Service{
		repository: repository, engine: engine, cipher: cipher,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(publicBaseURL), "/"),
		now:           time.Now, newID: ids.New,
	}
}

type CreateInput struct {
	TenantID, ProviderCode, ProviderVersion, Environment, Actor, RequestID string
}

func (s *Service) Create(ctx context.Context, in CreateInput) (model.Installation, error) {
	in.ProviderCode = strings.ToLower(strings.TrimSpace(in.ProviderCode))
	in.ProviderVersion = strings.TrimSpace(in.ProviderVersion)
	in.Environment = strings.ToLower(strings.TrimSpace(in.Environment))
	if in.ProviderCode == "" || !validEnvironment(in.Environment) {
		return model.Installation{}, &Error{Code: CodeValidationError, Message: "provider_code and environment (sandbox/live) are required"}
	}
	if in.ProviderVersion == "" {
		version, err := s.repository.GetReleasedProviderVersion(ctx, in.ProviderCode)
		if err != nil {
			return model.Installation{}, err
		}
		in.ProviderVersion = version
	}
	if _, err := s.engine.ManifestVersion(in.ProviderCode, in.ProviderVersion); err != nil {
		return model.Installation{}, &Error{
			Code: CodeProviderVersionNotRunning, Message: "requested provider version is not loaded by the connector runtime", Cause: err,
		}
	}
	id, err := s.newID("ins")
	if err != nil {
		return model.Installation{}, err
	}
	return s.repository.CreateInstallation(ctx, store.CreateInstallationInput{
		ID: id, TenantID: in.TenantID, ProviderCode: in.ProviderCode,
		ProviderVersion: in.ProviderVersion, Environment: in.Environment,
		ProfileID: "emisell-native", Actor: in.Actor, RequestID: in.RequestID,
	})
}

type ConfigureInput struct {
	TenantID              string
	InstallationID        string
	Credentials           map[string]string
	ClearFields           []string
	PaymentMethods        []map[string]any
	PaymentMethodsPresent bool
	Patch                 bool
	Actor                 string
	RequestID             string
}

func (s *Service) Configure(ctx context.Context, in ConfigureInput) (model.Installation, error) {
	installation, err := s.repository.GetInstallation(ctx, in.TenantID, in.InstallationID)
	if err != nil {
		return model.Installation{}, err
	}
	provider, err := s.repository.GetProvider(ctx, installation.ProviderCode)
	if err != nil {
		return model.Installation{}, err
	}
	if in.Patch && len(in.Credentials) == 0 && len(in.ClearFields) == 0 && !in.PaymentMethodsPresent {
		return model.Installation{}, &Error{Code: CodeNoCredentialChanges, Message: "credentials, clear_fields, or payment_methods must contain a change"}
	}
	if err = ValidateCredentialFieldNames(provider.CredentialSchema, in.ClearFields); err != nil {
		return model.Installation{}, &Error{Code: CodeInvalidCredentials, Message: err.Error(), Cause: err}
	}

	credentials := cloneCredentials(in.Credentials)
	if in.Patch {
		existing, loadErr := s.loadCredentials(ctx, in.TenantID, installation.ID)
		if loadErr != nil && !errors.Is(loadErr, store.ErrNotFound) {
			return model.Installation{}, loadErr
		}
		if existing == nil {
			existing = map[string]string{}
		}
		defer ClearCredentials(existing)
		for key, value := range credentials {
			if strings.TrimSpace(value) != "" {
				existing[key] = value
			}
		}
		for _, key := range in.ClearFields {
			delete(existing, key)
		}
		credentials = cloneCredentials(existing)
	}
	defer ClearCredentials(credentials)

	configuredAt := s.now().UTC()
	metadata, err := validateAndMaskCredentials(provider.CredentialSchema, credentials, configuredAt)
	if err != nil {
		return model.Installation{}, &Error{Code: CodeInvalidCredentials, Message: err.Error(), Cause: err}
	}
	manifest, err := s.engine.ManifestVersion(provider.Code, installation.ProviderVersion)
	if err != nil {
		return model.Installation{}, &Error{
			Code: CodeProviderVersionNotRunning, Message: "requested provider version is not loaded by the connector runtime", Cause: err,
		}
	}
	verificationID, err := s.newID("iver")
	if err != nil {
		return model.Installation{}, err
	}
	installation, err = s.repository.BeginCredentialConfig(ctx, in.TenantID, installation.ID, in.Actor, in.RequestID)
	if err != nil {
		return model.Installation{}, err
	}
	callbackURL := s.providerWebhookURL(provider.Code, installation.ID)
	result, err := s.engine.VerifyInstallation(ctx, connector.InstallationInput{
		InstallationID: installation.ID, ProviderCode: provider.Code,
		ProviderVersion: installation.ProviderVersion, Environment: installation.Environment,
		Credentials: credentials, PublicWebhookURL: callbackURL,
	})
	if err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, providerErrorCode(err), safeEngineError(err), &EngineError{Cause: err})
	}
	if !validEnvironment(result.Environment) {
		domainErr := &Error{Code: CodeCredentialModeUnknown, Message: "provider credential could not be identified as sandbox or live"}
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, domainErr.Code, "connector did not return a valid credential environment", domainErr)
	}
	if result.Environment != installation.Environment {
		domainErr := &Error{
			Code:    CodeCredentialModeMismatch,
			Message: fmt.Sprintf("credential belongs to %s; edit the %s credential slot instead", result.Environment, result.Environment),
		}
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, domainErr.Code, "credential environment does not match installation", domainErr)
	}

	storedCredentials := result.StoredCredentials
	if storedCredentials == nil {
		storedCredentials = cloneCredentials(credentials)
	}
	defer ClearCredentials(storedCredentials)
	credentialPayload, err := json.Marshal(storedCredentials)
	if err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "CREDENTIAL_SERIALIZATION_ERROR", "credential serialization failed", err)
	}
	ciphertext, err := s.cipher.Encrypt(credentialPayload, credentialAAD(installation.ID))
	clear(credentialPayload)
	if err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "CREDENTIAL_ENCRYPTION_ERROR", "credential encryption failed", err)
	}
	if err = s.repository.SaveProviderCredentials(ctx, in.TenantID, installation.ID, ciphertext); err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "CREDENTIAL_VAULT_ERROR", "credential vault write failed", err)
	}

	verifiedAt := s.now().UTC()
	var metadataObject map[string]any
	if err = json.Unmarshal(metadata, &metadataObject); err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "CREDENTIAL_METADATA_ERROR", "credential metadata could not be finalized", err)
	}
	delete(metadataObject, "verification_reason")
	delete(metadataObject, "previous_provider_version")
	metadataObject["execution_engine"] = "emisell_native"
	metadataObject["verified_environment"] = result.Environment
	metadataObject["webhook_ready"] = result.WebhookReady
	metadataObject["public_webhook_url"] = callbackURL
	metadataObject["verification_required"] = false
	metadataObject["verification"] = map[string]any{
		"id": verificationID, "result": "PASSED", "provider_version": installation.ProviderVersion,
		"manifest_digest": manifest.ExecutableSHA256, "verified_at": verifiedAt,
	}
	metadata, err = json.Marshal(metadataObject)
	if err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "CREDENTIAL_METADATA_ERROR", "credential metadata could not be finalized", err)
	}
	methods := installation.PaymentMethods
	if !in.Patch || in.PaymentMethodsPresent {
		methods, err = json.Marshal(in.PaymentMethods)
		if err != nil {
			return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "PAYMENT_METHOD_CONFIG_ERROR", "payment method configuration could not be finalized", err)
		}
	}
	item, err := s.repository.CompleteInstallationVerification(ctx, store.CompleteInstallationVerificationInput{
		ID: verificationID, TenantID: in.TenantID, InstallationID: installation.ID,
		ProviderCode: provider.Code, ProviderVersion: installation.ProviderVersion,
		Environment: installation.Environment, ManifestDigest: manifest.ExecutableSHA256,
		ConnectorID: result.ConnectorID, Metadata: metadata, PaymentMethods: methods,
		WebhookReady: result.WebhookReady, Actor: in.Actor, RequestID: in.RequestID,
		VerifiedAt: verifiedAt,
	})
	if err != nil {
		return model.Installation{}, s.failVerification(ctx, verificationID, installation, manifest, in, "VERIFICATION_FINALIZATION_ERROR", "credential configuration could not be finalized", err)
	}
	return item, nil
}

type UpgradeInput struct {
	TenantID, InstallationID, ProviderVersion, Actor, RequestID string
	ExpectedVersion                                             int64
}

func (s *Service) Upgrade(ctx context.Context, in UpgradeInput) (model.Installation, error) {
	in.ProviderVersion = strings.TrimSpace(in.ProviderVersion)
	if in.ExpectedVersion <= 0 || in.ProviderVersion == "" {
		return model.Installation{}, &Error{Code: CodeValidationError, Message: "version and provider_version are required"}
	}
	installation, err := s.repository.GetInstallation(ctx, in.TenantID, in.InstallationID)
	if err != nil {
		return model.Installation{}, err
	}
	if _, err = s.engine.ManifestVersion(installation.ProviderCode, in.ProviderVersion); err != nil {
		return model.Installation{}, &Error{
			Code: CodeProviderVersionNotRunning, Message: "requested provider version is not loaded by the connector runtime", Cause: err,
		}
	}
	return s.repository.UpgradeInstallation(ctx, in.TenantID, installation.ID, in.ProviderVersion, in.Actor, in.RequestID, in.ExpectedVersion)
}

type TransitionInput struct {
	TenantID, InstallationID, Actor, RequestID string
	ExpectedVersion                            int64
}

func (s *Service) Activate(ctx context.Context, in TransitionInput) (model.Installation, error) {
	return s.repository.TransitionInstallation(ctx, in.TenantID, in.InstallationID, model.InstallationActive, in.Actor, in.RequestID, in.ExpectedVersion)
}

func (s *Service) Deactivate(ctx context.Context, in TransitionInput) (model.Installation, error) {
	return s.repository.TransitionInstallation(ctx, in.TenantID, in.InstallationID, model.InstallationInactive, in.Actor, in.RequestID, in.ExpectedVersion)
}

type UninstallInput struct {
	TenantID, InstallationID, Actor, RequestID string
}

func (s *Service) Uninstall(ctx context.Context, in UninstallInput) (model.Installation, error) {
	installation, err := s.repository.GetInstallation(ctx, in.TenantID, in.InstallationID)
	if err != nil {
		return model.Installation{}, err
	}
	if installation.Status == model.InstallationUninstalled {
		return installation, nil
	}
	if installation.Status == model.InstallationActive {
		return model.Installation{}, &Error{Code: CodeInvalidState, Message: "deactivate the installation before uninstalling it"}
	}
	hasLiability, err := s.repository.HasOpenRefundLiability(ctx, in.TenantID, installation.ID)
	if err != nil {
		return model.Installation{}, err
	}
	if hasLiability {
		return model.Installation{}, &Error{
			Code:    CodeRefundLiabilityOpen,
			Message: "this connection still has refundable payments; keep it deactivated until the refund window and pending refunds are closed",
		}
	}
	credentials, credentialErr := s.loadCredentials(ctx, in.TenantID, installation.ID)
	if credentialErr == nil {
		defer ClearCredentials(credentials)
		err = s.engine.DisableInstallation(ctx, connector.InstallationInput{
			InstallationID: installation.ID, ProviderCode: installation.ProviderCode,
			ProviderVersion: installation.ProviderVersion, Environment: installation.Environment,
			Credentials: credentials, PublicWebhookURL: s.providerWebhookURL(installation.ProviderCode, installation.ID),
		})
		if err != nil && !errors.Is(err, connector.ErrNotSupported) {
			_ = s.repository.FailInstallation(ctx, in.TenantID, installation.ID, safeEngineError(err), in.Actor)
			return model.Installation{}, &EngineError{Cause: err}
		}
	} else if !errors.Is(credentialErr, store.ErrNotFound) {
		return model.Installation{}, credentialErr
	}
	if err = s.repository.DeleteProviderCredentials(ctx, in.TenantID, installation.ID); err != nil {
		return model.Installation{}, err
	}
	return s.repository.MarkUninstalled(ctx, in.TenantID, installation.ID, in.Actor, in.RequestID)
}

func (s *Service) failVerification(ctx context.Context, verificationID string, installation model.Installation, manifest connector.Manifest, in ConfigureInput, code, message string, cause error) error {
	failErr := s.repository.FailInstallationVerification(ctx, store.FailInstallationVerificationInput{
		ID: verificationID, TenantID: in.TenantID, InstallationID: installation.ID,
		ProviderCode: installation.ProviderCode, ProviderVersion: installation.ProviderVersion,
		Environment: installation.Environment, ManifestDigest: manifest.ExecutableSHA256,
		ErrorCode: code, ErrorMessage: message, Actor: in.Actor, RequestID: in.RequestID,
		VerifiedAt: s.now().UTC(),
	})
	if failErr != nil {
		return errors.Join(cause, fmt.Errorf("record installation verification failure: %w", failErr))
	}
	return cause
}

func (s *Service) loadCredentials(ctx context.Context, tenantID, installationID string) (map[string]string, error) {
	ciphertext, err := s.repository.GetProviderCredentials(ctx, tenantID, installationID)
	if err != nil {
		return nil, err
	}
	plaintext, err := s.cipher.Decrypt(ciphertext, credentialAAD(installationID))
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

func (s *Service) providerWebhookURL(providerCode, installationID string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	return s.publicBaseURL + "/webhooks/v1/providers/" + url.PathEscape(providerCode) + "/" + url.PathEscape(installationID)
}

type credentialField struct {
	Code     string `json:"code"`
	Secret   bool   `json:"secret"`
	Required bool   `json:"required"`
}

func ValidateAndMaskCredentials(schema json.RawMessage, credentials map[string]string) ([]byte, error) {
	return validateAndMaskCredentials(schema, credentials, time.Now().UTC())
}

func validateAndMaskCredentials(schema json.RawMessage, credentials map[string]string, configuredAt time.Time) ([]byte, error) {
	var fields []credentialField
	if err := json.Unmarshal(schema, &fields); err != nil {
		return nil, errors.New("provider credential schema is invalid")
	}
	allowed := make(map[string]credentialField, len(fields))
	for _, field := range fields {
		allowed[field.Code] = field
		if field.Required && strings.TrimSpace(credentials[field.Code]) == "" {
			return nil, fmt.Errorf("credential %s is required", field.Code)
		}
	}
	for key := range credentials {
		if _, ok := allowed[key]; !ok {
			return nil, fmt.Errorf("credential %s is not supported", key)
		}
	}
	codes := make([]string, 0, len(credentials))
	for key := range credentials {
		codes = append(codes, key)
	}
	sort.Strings(codes)
	configured := make([]map[string]any, 0, len(codes))
	for _, key := range codes {
		configured = append(configured, map[string]any{"code": key, "configured": strings.TrimSpace(credentials[key]) != ""})
	}
	return json.Marshal(map[string]any{"configured_fields": configured, "configured_at": configuredAt})
}

func ValidateCredentialFieldNames(schema json.RawMessage, names []string) error {
	var fields []credentialField
	if err := json.Unmarshal(schema, &fields); err != nil {
		return errors.New("provider credential schema is invalid")
	}
	allowed := make(map[string]bool, len(fields))
	for _, field := range fields {
		allowed[field.Code] = true
	}
	for _, name := range names {
		if !allowed[name] {
			return fmt.Errorf("credential %s is not supported", name)
		}
	}
	return nil
}

func ClearCredentials(values map[string]string) {
	for key := range values {
		values[key] = ""
		delete(values, key)
	}
}

func cloneCredentials(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func credentialAAD(installationID string) []byte {
	return []byte("provider-credential:" + installationID)
}

func validEnvironment(value string) bool {
	return value == model.EnvironmentSandbox || value == model.EnvironmentLive
}

func providerErrorCode(err error) string {
	var apiErr *connector.APIError
	if errors.As(err, &apiErr) && strings.TrimSpace(apiErr.Code) != "" {
		return apiErr.Code
	}
	if errors.Is(err, connector.ErrInvalidCredential) {
		return "INVALID_PROVIDER_CREDENTIAL"
	}
	return "ENGINE_ERROR"
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
