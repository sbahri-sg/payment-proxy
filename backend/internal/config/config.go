package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv                 string
	HTTPAddr               string
	APIRequestTimeout      time.Duration
	APIRateLimitRPS        int
	APIRateLimitBurst      int
	APIMaxInFlight         int
	DatabaseURL            string
	DatabaseMaxConns       int
	DatabaseMinConns       int
	DatabaseMaxLifetime    time.Duration
	DatabaseMaxIdleTime    time.Duration
	ServiceAPIKey          string
	AdminAPIKey            string
	CORSAllowedOrigins     []string
	PublicBaseURL          string
	CertificationReturnURL string
	ConnectorTimeout       time.Duration
	ConnectorRunners       []ConnectorRuntime
	ConnectorTLSCAPEM      []byte
	CredentialKey          string
	EmisellWebhookURL      string
	EmisellWebhookSecret   string
	WebhookAllowHTTP       bool
	WebhookAllowPrivate    bool
	WebhookPollInterval    time.Duration
	WebhookTimeout         time.Duration
	WebhookBaseBackoff     time.Duration
	WebhookMaxAttempts     int
	WebhookBatchSize       int
	EmisellReceiverAddr    string
	EmisellReceiverSecret  string
	EmisellReceiverSkew    time.Duration
}

type ConnectorRuntime struct {
	BaseURL string
	Token   string
}

func Load() (Config, error) {
	appEnv := envOr("APP_ENV", "development")
	connectorRunners, err := loadConnectorRuntimes()
	if err != nil {
		return Config{}, err
	}
	connectorTLSCAPEM, err := optionalBase64("CONNECTOR_TLS_CA_BASE64")
	if err != nil {
		return Config{}, err
	}
	rateLimitRPS := 0
	rateLimitBurst := 0
	maxInFlight := 0
	if appEnv == "production" {
		rateLimitRPS = 300
		rateLimitBurst = 600
		maxInFlight = 500
	}
	cfg := Config{
		AppEnv:                 appEnv,
		HTTPAddr:               ":" + envOr("API_PORT", "8080"),
		APIRequestTimeout:      seconds("API_REQUEST_TIMEOUT_SECONDS", 25),
		APIRateLimitRPS:        nonNegativeInteger("API_RATE_LIMIT_RPS", rateLimitRPS),
		APIRateLimitBurst:      nonNegativeInteger("API_RATE_LIMIT_BURST", rateLimitBurst),
		APIMaxInFlight:         nonNegativeInteger("API_MAX_IN_FLIGHT", maxInFlight),
		DatabaseURL:            strings.TrimSpace(os.Getenv("DATABASE_URL")),
		DatabaseMaxConns:       integer("DATABASE_MAX_CONNECTIONS", 40),
		DatabaseMinConns:       integer("DATABASE_MIN_CONNECTIONS", 4),
		DatabaseMaxLifetime:    seconds("DATABASE_CONNECTION_MAX_LIFETIME_SECONDS", 3600),
		DatabaseMaxIdleTime:    seconds("DATABASE_CONNECTION_MAX_IDLE_SECONDS", 300),
		ServiceAPIKey:          strings.TrimSpace(os.Getenv("SERVICE_API_KEY")),
		AdminAPIKey:            strings.TrimSpace(os.Getenv("ADMIN_API_KEY")),
		CORSAllowedOrigins:     splitCSV(os.Getenv("CORS_ALLOWED_ORIGINS")),
		PublicBaseURL:          strings.TrimRight(strings.TrimSpace(os.Getenv("PAYMENT_PROXY_PUBLIC_BASE_URL")), "/"),
		CertificationReturnURL: strings.TrimRight(strings.TrimSpace(os.Getenv("CERTIFICATION_RETURN_URL")), "/"),
		ConnectorTimeout:       seconds("CONNECTOR_TIMEOUT_SECONDS", 15),
		ConnectorRunners:       connectorRunners,
		ConnectorTLSCAPEM:      connectorTLSCAPEM,
		CredentialKey:          strings.TrimSpace(os.Getenv("CREDENTIAL_ENCRYPTION_KEY")),
		EmisellWebhookURL:      firstNonEmptyEnv("EMISELL_BACKEND_WEBHOOK_URL", "EMISELL_WEBHOOK_URL"),
		EmisellWebhookSecret:   firstNonEmptyEnv("EMISELL_BACKEND_WEBHOOK_SECRET", "EMISELL_WEBHOOK_SECRET"),
		WebhookAllowHTTP:       boolean("WEBHOOK_ALLOW_INSECURE_HTTP", false),
		WebhookAllowPrivate:    boolean("WEBHOOK_ALLOW_PRIVATE_NETWORKS", false),
		WebhookPollInterval:    seconds("WEBHOOK_POLL_INTERVAL_SECONDS", 2),
		WebhookTimeout:         seconds("WEBHOOK_DELIVERY_TIMEOUT_SECONDS", 10),
		WebhookBaseBackoff:     seconds("WEBHOOK_BASE_BACKOFF_SECONDS", 5),
		WebhookMaxAttempts:     integer("WEBHOOK_MAX_ATTEMPTS", 8),
		WebhookBatchSize:       integer("WEBHOOK_BATCH_SIZE", 20),
		EmisellReceiverAddr:    ":" + envOr("EMISELL_RECEIVER_PORT", "19090"),
		EmisellReceiverSecret:  firstNonEmptyEnv("EMISELL_RECEIVER_SECRET", "EMISELL_BACKEND_WEBHOOK_SECRET", "EMISELL_WEBHOOK_SECRET"),
		EmisellReceiverSkew:    seconds("EMISELL_RECEIVER_MAX_SKEW_SECONDS", 300),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return Config{}, errors.New("DATABASE_MIN_CONNECTIONS must not exceed DATABASE_MAX_CONNECTIONS")
	}
	if (cfg.APIRateLimitRPS == 0) != (cfg.APIRateLimitBurst == 0) {
		return Config{}, errors.New("API_RATE_LIMIT_RPS and API_RATE_LIMIT_BURST must both be zero or both be positive")
	}
	if cfg.PublicBaseURL != "" {
		publicURL, publicErr := url.Parse(cfg.PublicBaseURL)
		if publicErr != nil || publicURL.Host == "" || publicURL.Scheme != "https" || publicURL.User != nil {
			return Config{}, errors.New("PAYMENT_PROXY_PUBLIC_BASE_URL must be a public HTTPS URL")
		}
	}
	if cfg.CertificationReturnURL != "" {
		returnURL, returnErr := url.Parse(cfg.CertificationReturnURL)
		if returnErr != nil || returnURL.Host == "" || returnURL.Scheme != "https" || returnURL.User != nil || returnURL.Fragment != "" {
			return Config{}, errors.New("CERTIFICATION_RETURN_URL must be an HTTPS URL without credentials or fragment")
		}
	}
	return cfg, nil
}

func loadConnectorRuntimes() ([]ConnectorRuntime, error) {
	baseURLs := splitCSV(firstNonEmptyEnv("CONNECTOR_RUNNER_BASE_URLS", "CONNECTOR_RUNNER_BASE_URL"))
	if len(baseURLs) == 0 {
		baseURLs = []string{"http://connector-runner:18081"}
	}
	tokens := splitCSV(os.Getenv("CONNECTOR_RUNNER_TOKENS"))
	if len(tokens) == 0 {
		fallback := strings.TrimSpace(os.Getenv("CONNECTOR_RUNNER_TOKEN"))
		tokens = make([]string, len(baseURLs))
		for index := range tokens {
			tokens[index] = fallback
		}
	}
	if len(tokens) != len(baseURLs) {
		return nil, errors.New("CONNECTOR_RUNNER_TOKENS must contain one token for each CONNECTOR_RUNNER_BASE_URLS entry")
	}
	result := make([]ConnectorRuntime, 0, len(baseURLs))
	seen := map[string]bool{}
	for index, raw := range baseURLs {
		baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, errors.New("CONNECTOR_RUNNER_BASE_URLS entries must be HTTP(S) URLs without credentials")
		}
		if seen[baseURL] {
			return nil, errors.New("CONNECTOR_RUNNER_BASE_URLS must not contain duplicate URLs")
		}
		seen[baseURL] = true
		result = append(result, ConnectorRuntime{BaseURL: baseURL, Token: strings.TrimSpace(tokens[index])})
	}
	return result, nil
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	result := make([]string, 0)
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func boolean(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func integer(name string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func nonNegativeInteger(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func seconds(name string, fallback int) time.Duration {
	return time.Duration(integer(name, fallback)) * time.Second
}

func (c Config) ValidateRuntime() error {
	missing := make([]string, 0)
	for name, value := range map[string]string{
		"CREDENTIAL_ENCRYPTION_KEY": c.CredentialKey,
		"SERVICE_API_KEY":           c.ServiceAPIKey,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	for _, runtime := range c.ConnectorRunners {
		if runtime.Token == "" {
			missing = append(missing, "CONNECTOR_RUNNER_TOKENS")
			break
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("runtime configuration missing: %s", strings.Join(missing, ", "))
	}
	key, err := base64.StdEncoding.DecodeString(c.CredentialKey)
	if err != nil || len(key) != 32 {
		return errors.New("CREDENTIAL_ENCRYPTION_KEY must be base64 for exactly 32 bytes")
	}
	if c.AppEnv == "production" {
		if c.APIRateLimitRPS == 0 || c.APIRateLimitBurst == 0 || c.APIMaxInFlight == 0 {
			return errors.New("API traffic guards must be enabled in production")
		}
		if c.APIRequestTimeout <= c.ConnectorTimeout {
			return errors.New("API_REQUEST_TIMEOUT_SECONDS must exceed CONNECTOR_TIMEOUT_SECONDS in production")
		}
		if uniformBytes(key) {
			return errors.New("CREDENTIAL_ENCRYPTION_KEY must be randomly generated in production")
		}
		if weakProductionSecret(c.ServiceAPIKey) {
			return errors.New("SERVICE_API_KEY must be a non-default secret with at least 32 characters in production")
		}
		if weakProductionSecret(c.AdminAPIKey) {
			return errors.New("ADMIN_API_KEY must be a non-default secret with at least 32 characters in production")
		}
		for _, runtime := range c.ConnectorRunners {
			if weakProductionSecret(runtime.Token) {
				return errors.New("every connector runtime token must be a non-default secret with at least 32 characters in production")
			}
			runnerURL, _ := url.Parse(runtime.BaseURL)
			if runnerURL == nil || runnerURL.Scheme != "https" {
				return errors.New("every CONNECTOR_RUNNER_BASE_URLS entry must use HTTPS in production")
			}
		}
		if len(c.ConnectorTLSCAPEM) == 0 {
			return errors.New("CONNECTOR_TLS_CA_BASE64 is required in production")
		}
		if c.PublicBaseURL == "" {
			return errors.New("PAYMENT_PROXY_PUBLIC_BASE_URL is required in production")
		}
		if c.CertificationReturnURL == "" {
			return errors.New("CERTIFICATION_RETURN_URL is required in production")
		}
		if c.WebhookAllowHTTP || c.WebhookAllowPrivate {
			return errors.New("insecure or private webhook destinations must be disabled in production")
		}
	}
	return nil
}

type ConnectorRunnerConfig struct {
	AppEnv           string
	HTTPAddr         string
	Token            string
	XenditBaseURL    string
	ConnectorTimeout time.Duration
	TLSCertPEM       []byte
	TLSKeyPEM        []byte
}

func LoadConnectorRunner() (ConnectorRunnerConfig, error) {
	tlsCertPEM, tlsKeyPEM, err := connectorTLSKeyPair()
	if err != nil {
		return ConnectorRunnerConfig{}, err
	}
	cfg := ConnectorRunnerConfig{
		AppEnv:           envOr("APP_ENV", "development"),
		HTTPAddr:         ":" + envOr("CONNECTOR_RUNNER_PORT", "18081"),
		Token:            strings.TrimSpace(os.Getenv("CONNECTOR_RUNNER_TOKEN")),
		XenditBaseURL:    strings.TrimRight(envOr("XENDIT_BASE_URL", "https://api.xendit.co"), "/"),
		ConnectorTimeout: seconds("CONNECTOR_TIMEOUT_SECONDS", 15),
		TLSCertPEM:       tlsCertPEM,
		TLSKeyPEM:        tlsKeyPEM,
	}
	providerURLs := map[string]string{
		"XENDIT_BASE_URL": cfg.XenditBaseURL,
	}
	for name, value := range providerURLs {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return ConnectorRunnerConfig{}, fmt.Errorf("%s must be an HTTP(S) URL without credentials", name)
		}
		if cfg.AppEnv == "production" && parsed.Scheme != "https" {
			return ConnectorRunnerConfig{}, fmt.Errorf("%s must use HTTPS in production", name)
		}
	}
	if cfg.Token == "" {
		return ConnectorRunnerConfig{}, errors.New("CONNECTOR_RUNNER_TOKEN is required")
	}
	if cfg.AppEnv == "production" {
		if weakProductionSecret(cfg.Token) {
			return ConnectorRunnerConfig{}, errors.New("CONNECTOR_RUNNER_TOKEN must be a non-default secret with at least 32 characters in production")
		}
		if len(cfg.TLSCertPEM) == 0 {
			return ConnectorRunnerConfig{}, errors.New("CONNECTOR_TLS_CERT_BASE64 and CONNECTOR_TLS_KEY_BASE64 are required in production")
		}
	}
	return cfg, nil
}

type MidtransProviderAppConfig struct {
	AppEnv           string
	HTTPAddr         string
	Token            string
	SandboxBaseURL   string
	LiveBaseURL      string
	ConnectorTimeout time.Duration
	TLSCertPEM       []byte
	TLSKeyPEM        []byte
}

func LoadMidtransProviderApp() (MidtransProviderAppConfig, error) {
	tlsCertPEM, tlsKeyPEM, err := connectorTLSKeyPair()
	if err != nil {
		return MidtransProviderAppConfig{}, err
	}
	cfg := MidtransProviderAppConfig{
		AppEnv:           envOr("APP_ENV", "development"),
		HTTPAddr:         ":" + envOr("PROVIDER_APP_PORT", "18083"),
		Token:            strings.TrimSpace(os.Getenv("PROVIDER_APP_TOKEN")),
		SandboxBaseURL:   strings.TrimRight(envOr("MIDTRANS_SANDBOX_BASE_URL", "https://api.sandbox.midtrans.com"), "/"),
		LiveBaseURL:      strings.TrimRight(envOr("MIDTRANS_LIVE_BASE_URL", "https://api.midtrans.com"), "/"),
		ConnectorTimeout: seconds("CONNECTOR_TIMEOUT_SECONDS", 15),
		TLSCertPEM:       tlsCertPEM,
		TLSKeyPEM:        tlsKeyPEM,
	}
	for name, value := range map[string]string{
		"MIDTRANS_SANDBOX_BASE_URL": cfg.SandboxBaseURL,
		"MIDTRANS_LIVE_BASE_URL":    cfg.LiveBaseURL,
	} {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return MidtransProviderAppConfig{}, fmt.Errorf("%s must be an HTTP(S) URL without credentials", name)
		}
		if cfg.AppEnv == "production" && parsed.Scheme != "https" {
			return MidtransProviderAppConfig{}, fmt.Errorf("%s must use HTTPS in production", name)
		}
	}
	if cfg.Token == "" {
		return MidtransProviderAppConfig{}, errors.New("PROVIDER_APP_TOKEN is required")
	}
	if cfg.AppEnv == "production" && weakProductionSecret(cfg.Token) {
		return MidtransProviderAppConfig{}, errors.New("PROVIDER_APP_TOKEN must be a non-default secret with at least 32 characters in production")
	}
	if cfg.AppEnv == "production" && len(cfg.TLSCertPEM) == 0 {
		return MidtransProviderAppConfig{}, errors.New("CONNECTOR_TLS_CERT_BASE64 and CONNECTOR_TLS_KEY_BASE64 are required in production")
	}
	return cfg, nil
}

func connectorTLSKeyPair() ([]byte, []byte, error) {
	certPEM, err := optionalBase64("CONNECTOR_TLS_CERT_BASE64")
	if err != nil {
		return nil, nil, err
	}
	keyPEM, err := optionalBase64("CONNECTOR_TLS_KEY_BASE64")
	if err != nil {
		return nil, nil, err
	}
	if (len(certPEM) == 0) != (len(keyPEM) == 0) {
		return nil, nil, errors.New("CONNECTOR_TLS_CERT_BASE64 and CONNECTOR_TLS_KEY_BASE64 must be configured together")
	}
	return certPEM, keyPEM, nil
}

func optionalBase64(name string) ([]byte, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be valid base64", name)
	}
	return decoded, nil
}

func weakProductionSecret(value string) bool {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	return len(value) < 32 || strings.Contains(lower, "changeme") || strings.Contains(lower, "change-before-production") || strings.HasPrefix(lower, "local-")
}

func uniformBytes(value []byte) bool {
	if len(value) == 0 {
		return true
	}
	for _, item := range value[1:] {
		if item != value[0] {
			return false
		}
	}
	return true
}
