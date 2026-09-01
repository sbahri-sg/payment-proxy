package model

import (
	"encoding/json"
	"time"
)

const (
	EnvironmentLive    = "live"
	EnvironmentSandbox = "sandbox"

	InstallationConfigRequired = "CONFIG_REQUIRED"
	InstallationVerifying      = "VERIFYING"
	InstallationReady          = "READY"
	InstallationActive         = "ACTIVE"
	InstallationInactive       = "INACTIVE"
	InstallationError          = "ERROR"
	InstallationUninstalled    = "UNINSTALLED"

	PaymentMethodAssignmentActive   = "ACTIVE"
	PaymentMethodAssignmentInactive = "INACTIVE"
	PaymentMethodSupportDocumented  = "DOCUMENTED"
	PaymentMethodSupportCertified   = "CERTIFIED"
	PaymentMethodSupportDisabled    = "DISABLED"

	PaymentCreated    = "CREATED"
	PaymentProcessing = "PROCESSING"
	PaymentPending    = "PENDING"
	PaymentSucceeded  = "SUCCEEDED"
	PaymentFailed     = "FAILED"
	PaymentCancelled  = "CANCELLED"
	PaymentExpired    = "EXPIRED"
	PaymentUnknown    = "UNKNOWN"
)

type Provider struct {
	Code             string          `json:"code"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Available        bool            `json:"available"`
	EngineConnector  string          `json:"connector_code"`
	CredentialSchema json.RawMessage `json:"credential_schema"`
	Environments     json.RawMessage `json:"environments"`
	PaymentMethods   json.RawMessage `json:"payment_methods"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type ProviderAppVersion struct {
	ID             string          `json:"id"`
	ProviderCode   string          `json:"provider_code"`
	ProviderName   string          `json:"provider_name"`
	Version        string          `json:"version"`
	Status         string          `json:"status"`
	Runtime        string          `json:"runtime"`
	SDKVersion     string          `json:"sdk_version"`
	FileName       string          `json:"file_name"`
	ContentType    string          `json:"content_type"`
	ArtifactSize   int64           `json:"artifact_size"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	Manifest       json.RawMessage `json:"manifest"`
	ScanReport     json.RawMessage `json:"scan_report"`
	ReviewNote     string          `json:"review_note,omitempty"`
	SubmittedBy    string          `json:"submitted_by"`
	ReviewedBy     string          `json:"reviewed_by,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	PublishedAt    *time.Time      `json:"published_at,omitempty"`
}

type ProviderAppProvider struct {
	ProviderCode     string    `json:"provider_code"`
	ProviderName     string    `json:"provider_name"`
	Description      string    `json:"description"`
	WebsiteURL       string    `json:"website_url,omitempty"`
	DocumentationURL string    `json:"documentation_url,omitempty"`
	SupportEmail     string    `json:"support_email,omitempty"`
	Status           string    `json:"status"`
	VersionCount     int       `json:"version_count"`
	ActiveVersion    string    `json:"active_version,omitempty"`
	LatestVersion    string    `json:"latest_version,omitempty"`
	LatestStatus     string    `json:"latest_status,omitempty"`
	CreatedBy        string    `json:"created_by"`
	UpdatedBy        string    `json:"updated_by"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Installation struct {
	ID                 string          `json:"id"`
	TenantID           string          `json:"merchant_id"`
	ProviderCode       string          `json:"provider_code"`
	ProviderName       string          `json:"provider_name,omitempty"`
	Environment        string          `json:"environment"`
	PublicWebhookURL   string          `json:"public_webhook_url,omitempty"`
	EngineProfileID    string          `json:"-"`
	EngineConnectorID  string          `json:"connector_id,omitempty"`
	ExecutionEngine    string          `json:"execution_engine"`
	ProviderVersion    string          `json:"provider_version"`
	Status             string          `json:"status"`
	CredentialMetadata json.RawMessage `json:"credential_metadata"`
	PaymentMethods     json.RawMessage `json:"payment_methods"`
	LastError          string          `json:"last_error,omitempty"`
	Version            int64           `json:"version"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	UninstalledAt      *time.Time      `json:"uninstalled_at,omitempty"`
}

type InstallationVerification struct {
	ID              string    `json:"id"`
	TenantID        string    `json:"-"`
	InstallationID  string    `json:"installation_id"`
	ProviderCode    string    `json:"provider_code"`
	ProviderVersion string    `json:"provider_version"`
	Environment     string    `json:"environment"`
	ManifestDigest  string    `json:"manifest_digest,omitempty"`
	Result          string    `json:"result"`
	ConnectorID     string    `json:"connector_id,omitempty"`
	WebhookReady    bool      `json:"webhook_ready"`
	ErrorCode       string    `json:"error_code,omitempty"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	VerifiedBy      string    `json:"verified_by"`
	RequestID       string    `json:"request_id,omitempty"`
	VerifiedAt      time.Time `json:"verified_at"`
}

type PaymentMethodAssignment struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"-"`
	Environment       string    `json:"environment"`
	PaymentMethodCode string    `json:"payment_method_code"`
	PaymentMethod     string    `json:"payment_method"`
	PaymentMethodType string    `json:"payment_method_type"`
	InstallationID    string    `json:"installation_id"`
	ProviderCode      string    `json:"provider_code"`
	ProviderName      string    `json:"provider_name"`
	Label             string    `json:"label"`
	Status            string    `json:"status"`
	Version           int64     `json:"version"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type PaymentOption struct {
	ID                string `json:"id"`
	Environment       string `json:"environment"`
	PaymentMethodCode string `json:"payment_method_code"`
	Category          string `json:"category"`
	Label             string `json:"label"`
}

type PaymentMethodCatalogItem struct {
	Code        string          `json:"code"`
	Category    string          `json:"category"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Countries   json.RawMessage `json:"countries"`
	Currencies  json.RawMessage `json:"currencies"`
	Active      bool            `json:"active"`
	SortOrder   int             `json:"sort_order"`
	Providers   json.RawMessage `json:"providers"`
}

type ProviderPaymentMethodCapability struct {
	ProviderCode        string          `json:"provider_code"`
	ProviderName        string          `json:"provider_name"`
	ProviderAvailable   bool            `json:"provider_available"`
	PaymentMethodCode   string          `json:"payment_method_code"`
	ProviderMethod      string          `json:"provider_method"`
	ProviderMethodType  string          `json:"provider_method_type"`
	ProviderChannelCode string          `json:"provider_channel_code"`
	SupportStatus       string          `json:"support_status"`
	SourceURL           string          `json:"source_url"`
	Metadata            json.RawMessage `json:"metadata"`
}

type ConnectorCertificationCheck struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ConnectorCertificationRun struct {
	ID                string          `json:"id"`
	InstallationID    string          `json:"installation_id"`
	ProviderCode      string          `json:"provider_code"`
	ProviderName      string          `json:"provider_name"`
	PaymentMethodCode string          `json:"payment_method_code"`
	PaymentMethodName string          `json:"payment_method_name"`
	Environment       string          `json:"environment"`
	Status            string          `json:"status"`
	Checks            json.RawMessage `json:"checks"`
	PaymentID         string          `json:"payment_id,omitempty"`
	Message           string          `json:"message,omitempty"`
	InitiatedBy       string          `json:"initiated_by"`
	StartedAt         time.Time       `json:"started_at"`
	CompletedAt       time.Time       `json:"completed_at"`
}

type PaymentSession struct {
	ID                    string          `json:"id"`
	TenantID              string          `json:"-"`
	InstallationID        string          `json:"installation_id"`
	PaymentOptionID       string          `json:"payment_option_id,omitempty"`
	PaymentMethodCode     string          `json:"payment_method_code,omitempty"`
	ProviderCode          string          `json:"provider_code"`
	ProviderVersion       string          `json:"provider_version"`
	Environment           string          `json:"environment"`
	MerchantReference     string          `json:"merchant_reference"`
	IdempotencyKey        string          `json:"-"`
	Amount                int64           `json:"amount"`
	Currency              string          `json:"currency"`
	Status                string          `json:"status"`
	Flags                 []string        `json:"flags"`
	EnginePaymentID       string          `json:"provider_payment_id,omitempty"`
	ConnectorTxnID        string          `json:"connector_transaction_id,omitempty"`
	ExecutionEngine       string          `json:"execution_engine"`
	NextAction            json.RawMessage `json:"next_action,omitempty"`
	LastError             string          `json:"last_error,omitempty"`
	ReconciliationCount   int             `json:"reconciliation_count"`
	LastReconciledAt      *time.Time      `json:"last_reconciled_at,omitempty"`
	LastReconciledBy      string          `json:"last_reconciled_by,omitempty"`
	LastReconciliationKey string          `json:"-"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

type IntegrationReadinessCheck struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type IntegrationReadiness struct {
	Environment        string                      `json:"environment"`
	Status             string                      `json:"status"`
	Passed             int                         `json:"passed"`
	Total              int                         `json:"total"`
	ResilienceEvidence bool                        `json:"resilience_evidence"`
	RecommendedAction  string                      `json:"recommended_action,omitempty"`
	Checks             []IntegrationReadinessCheck `json:"checks"`
}

type PaymentList struct {
	Items   []PaymentSession `json:"items"`
	Total   int64            `json:"total"`
	Limit   int              `json:"limit"`
	Offset  int              `json:"offset"`
	HasMore bool             `json:"has_more"`
}

type PaymentStatusEvent struct {
	ID        int64           `json:"id"`
	PaymentID string          `json:"payment_id"`
	Status    string          `json:"status"`
	Source    string          `json:"source"`
	Details   json.RawMessage `json:"details"`
	CreatedAt time.Time       `json:"created_at"`
}

type Refund struct {
	ID                string    `json:"id"`
	TenantID          string    `json:"-"`
	PaymentID         string    `json:"payment_id"`
	PaymentMethodCode string    `json:"payment_method_code,omitempty"`
	Amount            int64     `json:"amount"`
	Currency          string    `json:"currency"`
	Reason            string    `json:"reason"`
	RequestedBy       string    `json:"requested_by"`
	Status            string    `json:"status"`
	EngineRefundID    string    `json:"provider_refund_id,omitempty"`
	ExecutionEngine   string    `json:"execution_engine"`
	LastError         string    `json:"last_error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type OutboxEvent struct {
	ID           string
	TenantID     string
	EventType    string
	Payload      []byte
	AttemptCount int
	MaxAttempts  int
	CreatedAt    time.Time
}

type WebhookInboxItem struct {
	ID              string     `json:"id"`
	Source          string     `json:"source"`
	ExternalEventID string     `json:"external_event_id"`
	EventType       string     `json:"event_type"`
	AggregateType   string     `json:"aggregate_type"`
	AggregateID     string     `json:"aggregate_id"`
	PayloadSHA256   string     `json:"payload_sha256"`
	Status          string     `json:"status"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	ReceivedAt      time.Time  `json:"received_at"`
	ProcessedAt     *time.Time `json:"processed_at,omitempty"`
}

type WebhookDelivery struct {
	ID             string          `json:"id"`
	EventType      string          `json:"event_type"`
	AggregateType  string          `json:"aggregate_type"`
	AggregateID    string          `json:"aggregate_id"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	AttemptCount   int             `json:"attempt_count"`
	MaxAttempts    int             `json:"max_attempts"`
	AvailableAt    time.Time       `json:"available_at"`
	LastHTTPStatus *int            `json:"last_http_status,omitempty"`
	LastError      string          `json:"last_error,omitempty"`
	DeliveredAt    *time.Time      `json:"delivered_at,omitempty"`
	ReplayCount    int             `json:"replay_count"`
	LastReplayedAt *time.Time      `json:"last_replayed_at,omitempty"`
	LastReplayedBy string          `json:"last_replayed_by,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type WebhookInboxList struct {
	Items   []WebhookInboxItem `json:"items"`
	Counts  map[string]int64   `json:"counts"`
	Total   int64              `json:"total"`
	Limit   int                `json:"limit"`
	Offset  int                `json:"offset"`
	HasMore bool               `json:"has_more"`
}

type WebhookDeliveryList struct {
	Items   []WebhookDelivery `json:"items"`
	Counts  map[string]int64  `json:"counts"`
	Total   int64             `json:"total"`
	Limit   int               `json:"limit"`
	Offset  int               `json:"offset"`
	HasMore bool              `json:"has_more"`
}

type ReconciliationCase struct {
	ID                  string    `json:"id"`
	Kind                string    `json:"kind"`
	ResourceType        string    `json:"resource_type"`
	ResourceID          string    `json:"resource_id"`
	Title               string    `json:"title"`
	Reference           string    `json:"reference,omitempty"`
	ProviderCode        string    `json:"provider_code,omitempty"`
	Environment         string    `json:"environment,omitempty"`
	EngineReference     string    `json:"engine_reference,omitempty"`
	CurrentStatus       string    `json:"current_status"`
	Severity            string    `json:"severity"`
	RecommendedAction   string    `json:"recommended_action"`
	CanResolve          bool      `json:"can_resolve"`
	ReconciliationCount int       `json:"reconciliation_count"`
	LastError           string    `json:"last_error,omitempty"`
	DetectedAt          time.Time `json:"detected_at"`
}

type ReconciliationList struct {
	Items   []ReconciliationCase `json:"items"`
	Counts  map[string]int64     `json:"counts"`
	Total   int64                `json:"total"`
	Limit   int                  `json:"limit"`
	Offset  int                  `json:"offset"`
	HasMore bool                 `json:"has_more"`
}

type DashboardOverview struct {
	GeneratedAt        time.Time                 `json:"generated_at"`
	Summary            DashboardSummary          `json:"summary"`
	PaymentStatuses    []DashboardStatusMetric   `json:"payment_statuses"`
	VolumeDaily        []DashboardVolumeMetric   `json:"volume_daily"`
	Providers          []DashboardProviderMetric `json:"providers"`
	RecentPayments     []DashboardRecentPayment  `json:"recent_payments"`
	OperationalBacklog DashboardOperational      `json:"operational_backlog"`
}

type DashboardSummary struct {
	Payments24h          int64   `json:"payments_24h"`
	PreviousPayments24h  int64   `json:"previous_payments_24h"`
	SucceededVolume24h   int64   `json:"succeeded_volume_24h"`
	PreviousVolume24h    int64   `json:"previous_volume_24h"`
	SuccessRate24h       float64 `json:"success_rate_24h"`
	WebhookSuccessRate24 float64 `json:"webhook_success_rate_24h"`
	ActiveInstallations  int64   `json:"active_installations"`
}

type DashboardStatusMetric struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type DashboardVolumeMetric struct {
	Date   string `json:"date"`
	Amount int64  `json:"amount"`
	Count  int64  `json:"count"`
}

type DashboardProviderMetric struct {
	Code           string          `json:"code"`
	Name           string          `json:"name"`
	Available      bool            `json:"available"`
	PaymentMethods json.RawMessage `json:"payment_methods"`
	Payments24h    int64           `json:"payments_24h"`
	Succeeded24h   int64           `json:"succeeded_24h"`
	Failed24h      int64           `json:"failed_24h"`
	Volume24h      int64           `json:"volume_24h"`
}

type DashboardRecentPayment struct {
	ID                string    `json:"id"`
	MerchantReference string    `json:"merchant_reference"`
	ProviderCode      string    `json:"provider_code"`
	Environment       string    `json:"environment"`
	Amount            int64     `json:"amount"`
	Currency          string    `json:"currency"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

type DashboardOperational struct {
	UnknownPayments int64 `json:"unknown_payments"`
	PendingOutbox   int64 `json:"pending_outbox"`
	DeadOutbox      int64 `json:"dead_outbox"`
	FailedWebhooks  int64 `json:"failed_webhooks"`
}
