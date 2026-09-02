package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProcessWebhookDuplicateAndOutOfOrderIntegration(t *testing.T) {
	databaseURL := os.Getenv("PAYMENT_PROXY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PAYMENT_PROXY_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantID := "itest.webhook." + suffix
	installationID := "ins_itest_" + suffix
	paymentID := "pay_itest_" + suffix
	expiredPaymentID := "pay_itest_expired_" + suffix
	enginePaymentID := "pr-itest-" + suffix
	expiredEnginePaymentID := "pr-itest-expired-" + suffix
	eventID := "evt-itest-" + suffix
	lateEventID := eventID + "-late"
	lateSuccessEventID := eventID + "-late-success"
	requestHash := sha256.Sum256([]byte(paymentID))
	payloadHash := sha256.Sum256([]byte(eventID))

	_, err = pool.Exec(ctx, `
		INSERT INTO provider_installations(
			id,tenant_id,provider_code,provider_version,environment,engine_profile_id,
			status,created_by,updated_by
		) VALUES($1,$2,'xendit','emisell-xendit-v1','sandbox','integration','ACTIVE','integration','integration')
	`, installationID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO payment_sessions(
			id,tenant_id,installation_id,payment_method_code,provider_code,provider_version,environment,merchant_reference,
			idempotency_key,request_hash,amount,currency,status,engine_payment_id,execution_engine,metadata
		) VALUES($1,$2,$3,'qris','xendit','emisell-xendit-v1','sandbox',$4,$5,$6,1000000,'IDR','PENDING',$7,'emisell_native',jsonb_build_object('order_id',$4::text))
	`, paymentID, tenantID, installationID, "order-"+suffix, "idem-"+suffix, requestHash[:], enginePaymentID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM outbox_events WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM integration_evidence WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM webhook_inbox WHERE source='xendit' AND external_event_id IN ($1,$2,$3)`, eventID, lateEventID, lateSuccessEventID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM payment_status_history WHERE tenant_id=$1 AND payment_id IN ($2,$3)`, tenantID, paymentID, expiredPaymentID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM payment_sessions WHERE tenant_id=$1 AND id IN ($2,$3)`, tenantID, paymentID, expiredPaymentID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_installations WHERE tenant_id=$1 AND id=$2`, tenantID, installationID)
	})

	store := New(pool)
	first := WebhookInput{
		ID: "wh_itest_" + suffix, Source: "xendit", ExternalEventID: eventID,
		EventType: "payment.succeeded", EnginePaymentID: enginePaymentID, Status: model.PaymentSucceeded,
		PayloadHash: payloadHash[:], PayloadCiphertext: []byte("encrypted-test-payload"),
	}
	accepted, err := store.ProcessWebhook(ctx, first)
	if err != nil || !accepted {
		t.Fatalf("first webhook was not accepted: accepted=%t err=%v", accepted, err)
	}
	var outboxPayload []byte
	if err = pool.QueryRow(ctx, `SELECT payload FROM outbox_events WHERE deduplication_key=$1`, "xendit:"+eventID).Scan(&outboxPayload); err != nil {
		t.Fatal(err)
	}
	var canonicalEvent struct {
		Metadata map[string]any `json:"metadata"`
		Data     struct {
			Payment struct {
				PaymentMethodCode string `json:"payment_method_code"`
			} `json:"payment"`
		} `json:"data"`
	}
	if err = json.Unmarshal(outboxPayload, &canonicalEvent); err != nil {
		t.Fatal(err)
	}
	if canonicalEvent.Metadata["order_id"] != "order-"+suffix || canonicalEvent.Data.Payment.PaymentMethodCode != "qris" {
		t.Fatalf("canonical payment event lost correlation data: %#v", canonicalEvent)
	}
	duplicate := first
	duplicate.ID = first.ID + "_duplicate"
	accepted, err = store.ProcessWebhook(ctx, duplicate)
	if err != nil || accepted {
		t.Fatalf("duplicate webhook was not deduplicated: accepted=%t err=%v", accepted, err)
	}
	late := first
	late.ID = first.ID + "_late"
	late.ExternalEventID = lateEventID
	late.EventType = "payment.failed"
	late.Status = model.PaymentFailed
	accepted, err = store.ProcessWebhook(ctx, late)
	if err != nil || !accepted {
		t.Fatalf("unique late webhook was not recorded: accepted=%t err=%v", accepted, err)
	}

	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM payment_sessions WHERE tenant_id=$1 AND id=$2`, tenantID, paymentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != model.PaymentSucceeded {
		t.Fatalf("late failed event downgraded successful payment to %s", status)
	}
	var inboxCount, outboxCount, historyCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM webhook_inbox WHERE source='xendit' AND external_event_id IN ($1,$2)`, eventID, lateEventID).Scan(&inboxCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE tenant_id=$1 AND aggregate_id=$2`, tenantID, paymentID).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM payment_status_history WHERE tenant_id=$1 AND payment_id=$2`, tenantID, paymentID).Scan(&historyCount); err != nil {
		t.Fatal(err)
	}
	if inboxCount != 2 || outboxCount != 2 || historyCount != 1 {
		t.Fatalf("unexpected dedup evidence: inbox=%d outbox=%d history=%d", inboxCount, outboxCount, historyCount)
	}
	succeededWebhook, err := store.HasProcessedPaymentWebhook(ctx, tenantID, "xendit", paymentID, model.PaymentSucceeded)
	if err != nil || !succeededWebhook {
		t.Fatalf("succeeded webhook evidence was not recorded: found=%t err=%v", succeededWebhook, err)
	}
	failedWebhook, err := store.HasProcessedPaymentWebhook(ctx, tenantID, "xendit", paymentID, model.PaymentFailed)
	if err != nil || !failedWebhook {
		t.Fatalf("late failed webhook evidence was not recorded independently: found=%t err=%v", failedWebhook, err)
	}
	deliveredSucceeded, err := store.HasDeliveredPaymentOutbox(ctx, tenantID, paymentID, model.PaymentSucceeded)
	if err != nil {
		t.Fatal(err)
	}
	if deliveredSucceeded {
		t.Fatal("undelivered succeeded event was accepted as Emisell delivery evidence")
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO payment_sessions(
			id,tenant_id,installation_id,provider_code,provider_version,environment,merchant_reference,
			idempotency_key,request_hash,amount,currency,status,engine_payment_id,execution_engine
		) VALUES($1,$2,$3,'xendit','emisell-xendit-v1','sandbox',$4,$5,$6,1000000,'IDR','EXPIRED',$7,'emisell_native')
	`, expiredPaymentID, tenantID, installationID, "expired-order-"+suffix, "expired-idem-"+suffix, requestHash[:], expiredEnginePaymentID)
	if err != nil {
		t.Fatal(err)
	}
	lateSuccess := WebhookInput{
		ID: "wh_itest_late_success_" + suffix, Source: "xendit", ExternalEventID: lateSuccessEventID,
		EventType: "payment.succeeded", EnginePaymentID: expiredEnginePaymentID, Status: model.PaymentSucceeded,
		PayloadHash: payloadHash[:], PayloadCiphertext: []byte("encrypted-late-success-payload"),
	}
	accepted, err = store.ProcessWebhook(ctx, lateSuccess)
	if err != nil || !accepted {
		t.Fatalf("late-success webhook was not accepted: accepted=%t err=%v", accepted, err)
	}
	expiredPayment, err := store.GetPayment(ctx, tenantID, expiredPaymentID)
	if err != nil {
		t.Fatal(err)
	}
	if expiredPayment.Status != model.PaymentSucceeded || !contains(expiredPayment.Flags, "late_payment") {
		t.Fatalf("late payment was not represented canonically: status=%s flags=%#v", expiredPayment.Status, expiredPayment.Flags)
	}
	if err = store.RecordIntegrationEvidence(ctx, tenantID, model.EnvironmentSandbox, "idempotency_replay", map[string]any{"payment_id": paymentID}); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordIntegrationEvidence(ctx, tenantID, model.EnvironmentSandbox, "payment_status_read", map[string]any{"payment_id": paymentID}); err != nil {
		t.Fatal(err)
	}
	facts, err := store.GetIntegrationReadinessFacts(ctx, tenantID, model.EnvironmentSandbox)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.ResilienceObserved {
		t.Fatal("late payment was not visible as resilience evidence")
	}
	if !facts.ActiveInstallation || !facts.PaymentCreated || !facts.IdempotencyReplay || !facts.PaymentStatusRead {
		t.Fatalf("readiness evidence was not observed: %#v", facts)
	}
}
