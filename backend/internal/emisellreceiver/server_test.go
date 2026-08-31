package emisellreceiver

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/emisellwebhook"
)

type fakeStore struct {
	received int
	err      error
}

func (s *fakeStore) ReceiveEmisellEvent(_ context.Context, _ emisellwebhook.ReceivedEvent) (bool, error) {
	s.received++
	if s.err != nil {
		return false, s.err
	}
	return s.received == 1, nil
}

func TestReceiverVerifiesSignatureAndTreatsDuplicateAsSuccess(t *testing.T) {
	secret := "test-secret-that-is-at-least-thirty-two-characters"
	store := &fakeStore{}
	handler, err := New(secret, 5*time.Minute, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := emisellwebhook.MarshalEnvelope("evt_test_1", "payment.updated", "merchant_test", "payment", "pay_test_1", time.Now(), map[string]any{
		"payment": map[string]any{"id": "pay_test_1", "status": "SUCCEEDED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	timestamp := time.Now().Unix()
	for attempt, expected := range []int{http.StatusAccepted, http.StatusOK} {
		request := signedRequest(body, timestamp, secret)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("attempt %d returned %d, want %d: %s", attempt+1, response.Code, expected, response.Body.String())
		}
	}
	if store.received != 2 {
		t.Fatalf("receiver store was called %d times", store.received)
	}
}

func TestReceiverRejectsInvalidSignatureAndStaleTimestamp(t *testing.T) {
	handler, err := New("test-secret-that-is-at-least-thirty-two-characters", time.Minute, &fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := emisellwebhook.MarshalEnvelope("evt_test_1", "payment.updated", "merchant_test", "payment", "pay_test_1", time.Now(), map[string]any{
		"payment": map[string]any{"id": "pay_test_1", "status": "SUCCEEDED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := signedRequest(body, time.Now().Unix(), "wrong-secret-that-is-also-at-least-thirty-two")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature returned %d", response.Code)
	}

	request = signedRequest(body, time.Now().Add(-2*time.Minute).Unix(), "test-secret-that-is-at-least-thirty-two-characters")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("stale timestamp returned %d", response.Code)
	}
}

func TestReceiverRejectsEnvelopeHeaderMismatch(t *testing.T) {
	secret := "test-secret-that-is-at-least-thirty-two-characters"
	handler, err := New(secret, time.Minute, &fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	body, err := emisellwebhook.MarshalEnvelope("evt_body", "payment.updated", "merchant_test", "payment", "pay_test", time.Now(), map[string]any{"payment": map[string]string{"id": "pay_test"}})
	if err != nil {
		t.Fatal(err)
	}
	request := signedRequest(body, time.Now().Unix(), secret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("mismatched envelope returned %d: %s", response.Code, response.Body.String())
	}
}

func TestReceiverSeparatesImmutableConflictFromStorageFailure(t *testing.T) {
	secret := "test-secret-that-is-at-least-thirty-two-characters"
	body, err := emisellwebhook.MarshalEnvelope("evt_test_1", "payment.updated", "merchant_test", "payment", "pay_test_1", time.Now(), map[string]any{
		"payment": map[string]any{"id": "pay_test_1", "status": "SUCCEEDED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, test := range map[string]struct {
		storeError error
		want       int
	}{
		"immutable conflict": {storeError: emisellwebhook.ErrEventConflict, want: http.StatusConflict},
		"storage failure":    {storeError: errors.New("database unavailable"), want: http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			handler, newErr := New(secret, time.Minute, &fakeStore{err: test.storeError}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if newErr != nil {
				t.Fatal(newErr)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, signedRequest(body, time.Now().Unix(), secret))
			if response.Code != test.want {
				t.Fatalf("status=%d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func signedRequest(body []byte, timestamp int64, secret string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/webhooks/v1/payment-proxy", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(emisellwebhook.HeaderWebhookID, "evt_test_1")
	request.Header.Set(emisellwebhook.HeaderEventType, "payment.updated")
	request.Header.Set(emisellwebhook.HeaderMerchantID, "merchant_test")
	request.Header.Set(emisellwebhook.HeaderWebhookTimestamp, strconv.FormatInt(timestamp, 10))
	request.Header.Set(emisellwebhook.HeaderWebhookVersion, "1")
	request.Header.Set(emisellwebhook.HeaderWebhookSignature, emisellwebhook.Sign(secret, timestamp, body))
	return request
}
