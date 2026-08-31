package emisellreceiver

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
)

const maxBody = 1 << 20

var (
	eventIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	tenantPattern  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	eventPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
)

type Store interface {
	ReceiveEmisellEvent(context.Context, emisellwebhook.ReceivedEvent) (bool, error)
}

type Server struct {
	secret []byte
	skew   time.Duration
	store  Store
	log    *slog.Logger
	now    func() time.Time
}

func New(secret string, skew time.Duration, store Store, logger *slog.Logger) (http.Handler, error) {
	if len(strings.TrimSpace(secret)) < 32 {
		return nil, errors.New("EMISELL_RECEIVER_SECRET must contain at least 32 characters")
	}
	if skew <= 0 {
		return nil, errors.New("EMISELL_RECEIVER_MAX_SKEW_SECONDS must be positive")
	}
	server := &Server{secret: []byte(secret), skew: skew, store: store, log: logger, now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", server.health)
	mux.HandleFunc("POST /webhooks/v1/payment-proxy", server.receive)
	// Development compatibility alias. Production Emisell Backend should expose
	// /webhooks/v1/payment-proxy as the stable receiver contract.
	mux.HandleFunc("POST /events", server.receive)
	return securityHeaders(mux), nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "emisell-webhook-receiver"})
}

func (s *Server) receive(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(body) == 0 || len(body) > maxBody {
		writeProblem(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "event payload must be between 1 byte and 1 MiB")
		return
	}
	eventID := firstHeader(r, emisellwebhook.HeaderWebhookID, "X-Emisell-Event-ID")
	tenantID := firstHeader(r, emisellwebhook.HeaderMerchantID)
	eventType := firstHeader(r, emisellwebhook.HeaderEventType)
	if !eventIDPattern.MatchString(eventID) || !tenantPattern.MatchString(tenantID) || !eventPattern.MatchString(eventType) {
		writeProblem(w, http.StatusBadRequest, "INVALID_EVENT_HEADERS", "event ID, merchant ID, and event type are required")
		return
	}
	if version := firstHeader(r, emisellwebhook.HeaderWebhookVersion); version != "1" {
		writeProblem(w, http.StatusBadRequest, "UNSUPPORTED_WEBHOOK_VERSION", "X-Emisell-Webhook-Version must be 1")
		return
	}
	timestamp, err := strconv.ParseInt(firstHeader(r, emisellwebhook.HeaderWebhookTimestamp, "X-Emisell-Timestamp"), 10, 64)
	if err != nil || absDuration(s.now().Sub(time.Unix(timestamp, 0))) > s.skew {
		writeProblem(w, http.StatusUnauthorized, "STALE_TIMESTAMP", "event timestamp is outside the accepted window")
		return
	}
	if !emisellwebhook.VerifySignature(s.secret, timestamp, body, firstHeader(r, emisellwebhook.HeaderWebhookSignature, "X-Emisell-Signature")) {
		writeProblem(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "event signature is invalid")
		return
	}
	envelope, err := emisellwebhook.ParseAndValidate(body, eventID, eventType, tenantID)
	if err != nil || !eventPattern.MatchString(envelope.Resource.Type) || !eventIDPattern.MatchString(envelope.Resource.ID) {
		writeProblem(w, http.StatusBadRequest, "INVALID_EVENT_ENVELOPE", "event body must match signed webhook headers and contain a valid resource")
		return
	}
	digest := sha256.Sum256(body)
	inserted, err := s.store.ReceiveEmisellEvent(r.Context(), emisellwebhook.ReceivedEvent{
		ID: eventID, TenantID: tenantID, EventType: eventType,
		Payload: body, PayloadSHA256: digest[:], SourceTimestamp: timestamp,
	})
	if errors.Is(err, emisellwebhook.ErrEventConflict) {
		s.log.Warn("reject conflicting Emisell event", "event_id", eventID)
		writeProblem(w, http.StatusConflict, "EVENT_CONFLICT", "event ID already exists with different immutable data")
		return
	}
	if err != nil {
		s.log.Error("receive Emisell event", "event_id", eventID, "error", err)
		writeProblem(w, http.StatusInternalServerError, "RECEIVER_STORAGE_FAILED", "event could not be stored")
		return
	}
	status := http.StatusOK
	if inserted {
		status = http.StatusAccepted
	}
	writeJSON(w, status, map[string]any{"accepted": true, "duplicate": !inserted, "event_id": eventID})
}

func firstHeader(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeProblem(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
