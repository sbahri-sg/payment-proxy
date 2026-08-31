package webhooksettings

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
	"github.com/emisell/api-payment-proxy/internal/ids"
	"github.com/emisell/api-payment-proxy/internal/secrets"
)

type Service struct {
	repository   Repository
	cipher       *secrets.Cipher
	appEnv       string
	allowHTTP    bool
	allowPrivate bool
	fallback     Fallback
	client       *http.Client
	now          func() time.Time
}

func NewService(repository Repository, cipher *secrets.Cipher, appEnv string, allowHTTP, allowPrivate bool, timeout time.Duration, fallback Fallback) *Service {
	transport := &http.Transport{
		Proxy:             nil,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext:       safeDialer(allowPrivate),
		ForceAttemptHTTP2: true,
	}
	return &Service{
		repository: repository, cipher: cipher, appEnv: strings.ToLower(strings.TrimSpace(appEnv)),
		allowHTTP: allowHTTP, allowPrivate: allowPrivate,
		fallback: Fallback{Enabled: fallback.Enabled, CallbackURL: strings.TrimSpace(fallback.CallbackURL), Secret: strings.TrimSpace(fallback.Secret)},
		client: &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("webhook redirects are disabled")
		}},
		now: time.Now,
	}
}

func (s *Service) Get(ctx context.Context) (Settings, error) {
	stored, err := s.repository.Get(ctx)
	if err == nil {
		return stored.Settings, nil
	}
	if err != ErrNotConfigured {
		return Settings{}, err
	}
	configured := s.fallback.CallbackURL != "" || s.fallback.Secret != ""
	return Settings{
		Configured: configured, CallbackURL: s.fallback.CallbackURL,
		Enabled:          s.fallback.Enabled && s.fallback.CallbackURL != "" && s.fallback.Secret != "",
		SecretConfigured: s.fallback.Secret != "", SecretHint: fallbackSecretHint(s.fallback.Secret),
		Source: "environment",
	}, nil
}

func (s *Service) Update(ctx context.Context, callbackURL string, enabled bool, actor string) (Settings, error) {
	callbackURL = strings.TrimSpace(callbackURL)
	actor = normalizedActor(actor)
	if callbackURL != "" {
		if err := s.validateCallbackURL(callbackURL); err != nil {
			return Settings{}, err
		}
	}
	if enabled && callbackURL == "" {
		return Settings{}, ErrInvalidURL
	}
	if err := s.materializeFallback(ctx, actor); err != nil {
		return Settings{}, err
	}
	if enabled {
		stored, err := s.repository.Get(ctx)
		if err != nil {
			return Settings{}, err
		}
		if len(stored.SecretCiphertext) == 0 {
			return Settings{}, ErrSecretNotConfigured
		}
	}
	if err := s.repository.UpsertConfig(ctx, callbackURL, enabled, actor); err != nil {
		return Settings{}, err
	}
	return s.Get(ctx)
}

func (s *Service) GenerateSecret(ctx context.Context, actor string) (GeneratedSecret, error) {
	actor = normalizedActor(actor)
	if err := s.materializeFallback(ctx, actor); err != nil {
		return GeneratedSecret{}, err
	}
	stored, err := s.repository.Get(ctx)
	if err != nil {
		return GeneratedSecret{}, err
	}
	// Rotation is fail-closed. The receiver must be updated before delivery is re-enabled.
	if err = s.repository.UpsertConfig(ctx, stored.CallbackURL, false, actor); err != nil {
		return GeneratedSecret{}, err
	}
	randomValue := make([]byte, 32)
	if _, err = rand.Read(randomValue); err != nil {
		return GeneratedSecret{}, fmt.Errorf("generate Emisell webhook secret: %w", err)
	}
	secret := "whsec_" + base64.RawURLEncoding.EncodeToString(randomValue)
	clearBytes(randomValue)
	if err = s.persistSecret(ctx, secret, actor); err != nil {
		return GeneratedSecret{}, err
	}
	settings, err := s.Get(ctx)
	if err != nil {
		return GeneratedSecret{}, err
	}
	return GeneratedSecret{Settings: settings, Secret: secret}, nil
}

func (s *Service) ResolveWebhookDestination(ctx context.Context) (string, []byte, bool, error) {
	stored, err := s.repository.Get(ctx)
	if err == ErrNotConfigured {
		if !s.fallback.Enabled {
			return "", nil, false, nil
		}
		if s.fallback.CallbackURL == "" || len(s.fallback.Secret) < 32 {
			return "", nil, false, ErrNotConfigured
		}
		if err = s.validateCallbackURL(s.fallback.CallbackURL); err != nil {
			return "", nil, false, err
		}
		return s.fallback.CallbackURL, []byte(s.fallback.Secret), true, nil
	}
	if err != nil {
		return "", nil, false, err
	}
	if !stored.Enabled {
		return "", nil, false, nil
	}
	if err = s.validateCallbackURL(stored.CallbackURL); err != nil {
		return "", nil, false, err
	}
	plaintext, err := s.decrypt(stored)
	if err != nil {
		return "", nil, false, err
	}
	return stored.CallbackURL, plaintext, true, nil
}

func (s *Service) Test(ctx context.Context, actor string) (TestResult, error) {
	actor = normalizedActor(actor)
	if err := s.materializeFallback(ctx, actor); err != nil {
		return TestResult{}, err
	}
	stored, err := s.repository.Get(ctx)
	if err != nil {
		return TestResult{}, err
	}
	if err = s.validateCallbackURL(stored.CallbackURL); err != nil {
		return TestResult{}, err
	}
	secret, err := s.decrypt(stored)
	if err != nil {
		return TestResult{}, err
	}
	defer clearBytes(secret)
	eventID, err := ids.New("evt_test")
	if err != nil {
		return TestResult{}, err
	}
	testedAt := s.now().UTC()
	body, err := emisellwebhook.MarshalEnvelope(eventID, "webhook.test", "platform", "webhook", "connection", testedAt, map[string]any{
		"webhook": map[string]any{"id": "connection", "source": "payment-proxy-dashboard", "message": "Signed webhook connection test"},
	})
	if err != nil {
		return TestResult{}, fmt.Errorf("encode Emisell webhook test: %w", err)
	}
	timestamp := testedAt.Unix()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, stored.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return TestResult{}, fmt.Errorf("create Emisell webhook test request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set("User-Agent", "Emisell-Payment-Proxy/1.0")
	request.Header.Set("Idempotency-Key", eventID)
	request.Header.Set(emisellwebhook.HeaderWebhookID, eventID)
	request.Header.Set(emisellwebhook.HeaderWebhookTimestamp, strconv.FormatInt(timestamp, 10))
	request.Header.Set(emisellwebhook.HeaderWebhookSignature, emisellwebhook.Sign(string(secret), timestamp, body))
	request.Header.Set(emisellwebhook.HeaderWebhookVersion, "1")
	request.Header.Set(emisellwebhook.HeaderEventType, "webhook.test")
	request.Header.Set(emisellwebhook.HeaderMerchantID, "platform")

	response, requestErr := s.client.Do(request)
	if requestErr != nil {
		message := "Emisell Backend receiver could not be reached."
		_ = s.repository.RecordTest(ctx, testedAt, false, 0, message)
		return TestResult{Success: false, EventID: eventID, TestedAt: testedAt, Message: message}, nil
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	success := response.StatusCode >= 200 && response.StatusCode < 300
	message := "Signed webhook test was accepted by Emisell Backend."
	recordMessage := ""
	if !success {
		message = fmt.Sprintf("Emisell Backend receiver returned HTTP %d.", response.StatusCode)
		recordMessage = message
	}
	if err = s.repository.RecordTest(ctx, testedAt, success, response.StatusCode, recordMessage); err != nil {
		return TestResult{}, err
	}
	return TestResult{Success: success, HTTPStatus: response.StatusCode, EventID: eventID, TestedAt: testedAt, Message: message}, nil
}

func (s *Service) materializeFallback(ctx context.Context, actor string) error {
	_, err := s.repository.Get(ctx)
	if err == nil {
		return nil
	}
	if err != ErrNotConfigured {
		return err
	}
	if err = s.repository.UpsertConfig(ctx, s.fallback.CallbackURL, false, actor); err != nil {
		return err
	}
	if s.fallback.Secret != "" {
		if err = s.persistSecret(ctx, s.fallback.Secret, actor); err != nil {
			return err
		}
	}
	if s.fallback.Enabled && s.fallback.CallbackURL != "" && s.fallback.Secret != "" {
		return s.repository.UpsertConfig(ctx, s.fallback.CallbackURL, true, actor)
	}
	return nil
}

func (s *Service) persistSecret(ctx context.Context, secret, actor string) error {
	if len(secret) < 32 {
		return ErrSecretNotConfigured
	}
	fingerprint := sha256.Sum256([]byte(secret))
	fingerprintHex := hex.EncodeToString(fingerprint[:])
	ciphertext, err := s.cipher.Encrypt([]byte(secret), []byte("emisell-backend-webhook:"+fingerprintHex))
	if err != nil {
		return err
	}
	prefixLength := min(10, len(secret)-4)
	return s.repository.RotateSecret(ctx, SecretInput{
		Ciphertext: ciphertext, Fingerprint: fingerprint[:], Prefix: secret[:prefixLength],
		LastFour: secret[len(secret)-4:], Actor: normalizedActor(actor),
	})
}

func (s *Service) decrypt(stored StoredSettings) ([]byte, error) {
	if len(stored.SecretCiphertext) == 0 || len(stored.SecretFingerprint) == 0 {
		return nil, ErrSecretNotConfigured
	}
	fingerprintHex := hex.EncodeToString(stored.SecretFingerprint)
	return s.cipher.Decrypt(stored.SecretCiphertext, []byte("emisell-backend-webhook:"+fingerprintHex))
}

func (s *Service) validateCallbackURL(value string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return ErrInvalidURL
	}
	if parsed.Scheme != "https" && !(s.appEnv != "production" && s.allowHTTP && parsed.Scheme == "http") {
		return ErrInvalidURL
	}
	host := strings.ToLower(parsed.Hostname())
	if s.appEnv == "production" || !s.allowPrivate {
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return ErrInvalidURL
		}
		if ip := net.ParseIP(host); ip != nil && unsafeIP(ip) {
			return ErrInvalidURL
		}
	}
	return nil
}

func safeDialer(allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, candidate := range ips {
			if !allowPrivate && unsafeIP(candidate.IP) {
				continue
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
		}
		return nil, fmt.Errorf("webhook target did not resolve to an allowed IP")
	}
}

func unsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func normalizedActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return "system"
	}
	if len(actor) > 128 {
		return actor[:128]
	}
	return actor
}

func fallbackSecretHint(secret string) string {
	if secret == "" {
		return ""
	}
	return "Stored in environment"
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
