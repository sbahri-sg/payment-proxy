package outbox

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/config"
	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
	"github.com/emisell/api-payment-proxy/internal/ids"
	"github.com/emisell/api-payment-proxy/internal/store"
)

type Worker struct {
	store        *store.Postgres
	destination  DestinationResolver
	pollInterval time.Duration
	baseBackoff  time.Duration
	batchSize    int
	allowHTTP    bool
	workerID     string
	client       *http.Client
	log          *slog.Logger
}

type DestinationResolver interface {
	ResolveWebhookDestination(context.Context) (endpoint string, secret []byte, enabled bool, err error)
}

func New(cfg config.Config, database *store.Postgres, destination DestinationResolver, logger *slog.Logger) (*Worker, error) {
	workerID, err := ids.New("worker")
	if err != nil {
		return nil, err
	}
	if destination == nil {
		return nil, errors.New("webhook destination resolver is required")
	}
	transport := &http.Transport{
		Proxy:             nil,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext:       safeDialer(cfg.WebhookAllowPrivate),
		ForceAttemptHTTP2: true,
	}
	client := &http.Client{
		Timeout: cfg.WebhookTimeout, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("webhook redirects are disabled")
		},
	}
	return &Worker{
		store: database, destination: destination,
		pollInterval: cfg.WebhookPollInterval, baseBackoff: cfg.WebhookBaseBackoff,
		batchSize: cfg.WebhookBatchSize, allowHTTP: cfg.WebhookAllowHTTP,
		workerID: workerID, client: client, log: logger,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	w.log.Info("outbox worker started", "worker_id", w.workerID)
	for {
		target, secret, enabled, err := w.destination.ResolveWebhookDestination(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.log.Error("resolve Emisell Backend webhook destination", "error", err)
			if !wait(ctx, w.pollInterval) {
				return nil
			}
			continue
		}
		if !enabled {
			if !wait(ctx, w.pollInterval) {
				return nil
			}
			continue
		}
		if _, err = validateTarget(target, w.allowHTTP); err != nil || len(secret) < 32 {
			clearBytes(secret)
			w.log.Error("resolved Emisell Backend webhook destination is invalid")
			if !wait(ctx, w.pollInterval) {
				return nil
			}
			continue
		}
		processed := 0
		for processed < w.batchSize {
			event, claimErr := w.store.ClaimOutbox(ctx, w.workerID)
			if errors.Is(claimErr, store.ErrNotFound) {
				break
			}
			if claimErr != nil {
				if ctx.Err() != nil {
					clearBytes(secret)
					return nil
				}
				w.log.Error("claim outbox event", "error", claimErr)
				break
			}
			w.deliver(ctx, target, secret, event.ID, event.TenantID, event.EventType, event.Payload, event.AttemptCount, event.MaxAttempts)
			processed++
		}
		clearBytes(secret)
		if processed > 0 {
			continue
		}
		if !wait(ctx, w.pollInterval) {
			return nil
		}
	}
}

func (w *Worker) deliver(ctx context.Context, target string, secret []byte, id, tenantID, eventType string, payload []byte, attempt, maxAttempts int) {
	request, err := newDeliveryRequest(ctx, target, secret, id, tenantID, eventType, payload, time.Now())
	httpStatus := 0
	retryAfter := time.Duration(0)
	if err == nil {
		var response *http.Response
		response, err = w.client.Do(request)
		if response != nil {
			httpStatus = response.StatusCode
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				if dbErr := w.store.CompleteOutbox(ctx, id, response.StatusCode); dbErr != nil {
					w.log.Error("complete outbox event", "event_id", id, "error", dbErr)
				}
				return
			}
			retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
			err = fmt.Errorf("Emisell returned HTTP %d", response.StatusCode)
		}
	}
	dead := attempt >= maxAttempts || permanentHTTPFailure(httpStatus)
	available := time.Now().Add(backoff(w.baseBackoff, attempt))
	if retryAfter > 0 {
		available = time.Now().Add(retryAfter)
	}
	message := "delivery failed"
	if err != nil {
		message = err.Error()
	}
	if dbErr := w.store.RetryOutbox(ctx, id, httpStatus, message, available, dead); dbErr != nil {
		w.log.Error("reschedule outbox event", "event_id", id, "error", dbErr)
		return
	}
	w.log.Warn("outbox delivery failed", "event_id", id, "attempt", attempt, "dead", dead, "http_status", httpStatus)
}

func newDeliveryRequest(ctx context.Context, target string, secret []byte, id, tenantID, eventType string, payload []byte, now time.Time) (*http.Request, error) {
	if _, err := emisellwebhook.ParseAndValidate(payload, id, eventType, tenantID); err != nil {
		return nil, fmt.Errorf("invalid Emisell event envelope: %w", err)
	}
	timestamp := now.Unix()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	signature := emisellwebhook.Sign(string(secret), timestamp, payload)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cache-Control", "no-store")
	request.Header.Set("User-Agent", "Emisell-Payment-Proxy/1.0")
	request.Header.Set("Idempotency-Key", id)
	request.Header.Set(emisellwebhook.HeaderWebhookID, id)
	request.Header.Set(emisellwebhook.HeaderEventType, eventType)
	request.Header.Set(emisellwebhook.HeaderMerchantID, tenantID)
	request.Header.Set(emisellwebhook.HeaderWebhookTimestamp, strconv.FormatInt(timestamp, 10))
	request.Header.Set(emisellwebhook.HeaderWebhookVersion, "1")
	request.Header.Set(emisellwebhook.HeaderWebhookSignature, signature)
	// Temporary aliases ease migration of an existing Emisell receiver. New
	// integrations must use the X-Emisell-Webhook-* headers above.
	request.Header.Set("X-Emisell-Event-ID", id)
	request.Header.Set("X-Emisell-Timestamp", strconv.FormatInt(timestamp, 10))
	request.Header.Set("X-Emisell-Signature", signature)
	return request, nil
}

func wait(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func permanentHTTPFailure(status int) bool {
	if status == 0 || status == http.StatusRequestTimeout || status == http.StatusTooEarly || status == http.StatusTooManyRequests || status >= 500 {
		return false
	}
	return status >= 300
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return min(time.Duration(seconds)*time.Second, time.Hour)
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return min(when.Sub(now), time.Hour)
}

func validateTarget(raw string, allowHTTP bool) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("EMISELL_WEBHOOK_URL is invalid")
	}
	if parsed.Scheme != "https" && !(allowHTTP && parsed.Scheme == "http") {
		return nil, errors.New("EMISELL_WEBHOOK_URL must use HTTPS")
	}
	if parsed.User != nil {
		return nil, errors.New("EMISELL_WEBHOOK_URL must not contain user info")
	}
	return parsed, nil
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
		return nil, errors.New("webhook target did not resolve to an allowed IP")
	}
}

func unsafeIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func backoff(base time.Duration, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 10 {
		shift = 10
	}
	value := base * time.Duration(1<<shift)
	if value > time.Hour {
		value = time.Hour
	}
	jitter := time.Duration(rand.Int64N(max(int64(value/4), 1)))
	return value + jitter
}
