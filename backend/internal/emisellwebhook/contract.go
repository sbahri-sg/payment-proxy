package emisellwebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrEventConflict = errors.New("webhook event ID already exists with different immutable data")

const (
	ContractVersion  = "2026-08-28"
	SignatureVersion = "v1"

	HeaderWebhookID        = "X-Emisell-Webhook-ID"
	HeaderWebhookTimestamp = "X-Emisell-Webhook-Timestamp"
	HeaderWebhookSignature = "X-Emisell-Webhook-Signature"
	HeaderWebhookVersion   = "X-Emisell-Webhook-Version"
	HeaderEventType        = "X-Emisell-Event-Type"
	HeaderMerchantID       = "X-Emisell-Merchant-ID"
)

type Resource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type Envelope struct {
	ID         string          `json:"id"`
	Object     string          `json:"object"`
	APIVersion string          `json:"api_version"`
	Type       string          `json:"type"`
	CreatedAt  time.Time       `json:"created_at"`
	MerchantID string          `json:"merchant_id"`
	Resource   Resource        `json:"resource"`
	Data       json.RawMessage `json:"data"`
}

type ReceivedEvent struct {
	ID              string
	TenantID        string
	EventType       string
	Payload         json.RawMessage
	PayloadSHA256   []byte
	SourceTimestamp int64
}

func MarshalEnvelope(id, eventType, merchantID, aggregateType, aggregateID string, createdAt time.Time, data any) ([]byte, error) {
	dataJSON, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	envelope := Envelope{
		ID: id, Object: "event", APIVersion: ContractVersion, Type: eventType,
		CreatedAt: createdAt.UTC(), MerchantID: merchantID,
		Resource: Resource{Type: aggregateType, ID: aggregateID}, Data: dataJSON,
	}
	if err = ValidateEnvelope(envelope, id, eventType, merchantID); err != nil {
		return nil, err
	}
	return json.Marshal(envelope)
}

func ParseAndValidate(body []byte, eventID, eventType, merchantID string) (Envelope, error) {
	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Envelope{}, errors.New("event body must be valid JSON")
	}
	if err := ValidateEnvelope(envelope, eventID, eventType, merchantID); err != nil {
		return Envelope{}, err
	}
	return envelope, nil
}

func ValidateEnvelope(envelope Envelope, eventID, eventType, merchantID string) error {
	if envelope.ID == "" || envelope.ID != eventID {
		return errors.New("event body ID does not match webhook ID")
	}
	if envelope.Object != "event" {
		return errors.New("event object must be event")
	}
	if envelope.APIVersion != ContractVersion {
		return errors.New("unsupported event API version")
	}
	if envelope.Type == "" || envelope.Type != eventType {
		return errors.New("event body type does not match event header")
	}
	if envelope.MerchantID == "" || envelope.MerchantID != merchantID {
		return errors.New("event body merchant does not match merchant header")
	}
	if envelope.CreatedAt.IsZero() || envelope.Resource.Type == "" || envelope.Resource.ID == "" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("event created_at, resource, and data are required")
	}
	if !strings.HasPrefix(envelope.Type, envelope.Resource.Type+".") {
		return errors.New("event type does not match resource type")
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return errors.New("event data must be a JSON object")
	}
	objectJSON, ok := data[envelope.Resource.Type]
	if !ok {
		return errors.New("event data does not contain the canonical resource")
	}
	var object struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(objectJSON, &object); err != nil || object.ID != envelope.Resource.ID {
		return errors.New("event data resource ID does not match resource reference")
	}
	return nil
}

func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	_, _ = mac.Write(body)
	return SignatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifySignature(secret []byte, timestamp int64, body []byte, signature string) bool {
	prefix := SignatureVersion + "="
	signature = strings.TrimSpace(signature)
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, prefix))
	if err != nil {
		return false
	}
	expected := Sign(string(secret), timestamp, body)
	expectedBytes, _ := hex.DecodeString(strings.TrimPrefix(expected, prefix))
	return hmac.Equal(provided, expectedBytes)
}
