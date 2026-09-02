package emisellwebhook

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeIdentityAndSignature(t *testing.T) {
	createdAt := time.Date(2026, 8, 28, 9, 30, 0, 0, time.UTC)
	body, err := MarshalEnvelope("evt_test", "payment.updated", "merchant_test", "payment", "pay_test", createdAt, map[string]any{
		"payment": map[string]any{"id": "pay_test", "status": "SUCCEEDED"},
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := ParseAndValidate(body, "evt_test", "payment.updated", "merchant_test")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Resource.ID != "pay_test" || envelope.APIVersion != ContractVersion || !json.Valid(envelope.Data) {
		t.Fatalf("unexpected envelope: %+v", envelope)
	}
	signature := Sign("test-secret-that-is-at-least-thirty-two-characters", 12345, body)
	if !VerifySignature([]byte("test-secret-that-is-at-least-thirty-two-characters"), 12345, body, signature) {
		t.Fatal("valid signature was rejected")
	}
	if VerifySignature([]byte("test-secret-that-is-at-least-thirty-two-characters"), 12345, append(body, ' '), signature) {
		t.Fatal("modified body signature was accepted")
	}
}

func TestEnvelopeRejectsHeaderMismatch(t *testing.T) {
	body, err := MarshalEnvelope("evt_test", "payment.updated", "merchant_test", "payment", "pay_test", time.Now(), map[string]any{"payment": map[string]string{"id": "pay_test"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseAndValidate(body, "evt_other", "payment.updated", "merchant_test"); err == nil {
		t.Fatal("mismatched webhook ID was accepted")
	}
}

func TestEnvelopeCarriesMerchantMetadata(t *testing.T) {
	body, err := MarshalEnvelopeWithMetadata(
		"evt_metadata", "payment.updated", "merchant_test", "payment", "pay_test", time.Now(),
		map[string]any{"order_id": "order_2026_0001"},
		map[string]any{"payment": map[string]any{"id": "pay_test", "payment_method_code": "qris"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := ParseAndValidate(body, "evt_metadata", "payment.updated", "merchant_test")
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err = json.Unmarshal(envelope.Metadata, &metadata); err != nil || metadata["order_id"] != "order_2026_0001" {
		t.Fatalf("unexpected metadata: %#v, %v", metadata, err)
	}
	var data struct {
		Payment struct {
			PaymentMethodCode string `json:"payment_method_code"`
		} `json:"payment"`
	}
	if err = json.Unmarshal(envelope.Data, &data); err != nil || data.Payment.PaymentMethodCode != "qris" {
		t.Fatalf("unexpected payment data: %#v, %v", data, err)
	}
}
