package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWebhookAdminVisibilityAndReplayIntegration(t *testing.T) {
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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	tenantA, tenantB := "itest.webhook.a."+suffix, "itest.webhook.b."+suffix
	inboxIDs := []string{"wh_admin_a_" + suffix, "wh_admin_b_" + suffix, "wh_admin_orphan_" + suffix}
	deliveryIDs := []string{"evt_admin_a_" + suffix, "evt_admin_b_" + suffix}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit_logs WHERE resource_id=ANY($1::text[])`, deliveryIDs)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM outbox_events WHERE id=ANY($1::text[])`, deliveryIDs)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM webhook_inbox WHERE id=ANY($1::text[])`, inboxIDs)
	})
	for i, tenantID := range []string{tenantA, tenantB, ""} {
		status := "PROCESSED"
		if tenantID == "" {
			status = "IGNORED"
		}
		hash := sha256.Sum256([]byte(inboxIDs[i]))
		_, err = pool.Exec(ctx, `INSERT INTO webhook_inbox(id,tenant_id,source,external_event_id,event_type,canonical_status,status,payload_sha256,payload_ciphertext) VALUES($1,$2,'xendit',$1,'payment.capture','SUCCEEDED',$3,$4,$5)`, inboxIDs[i], tenantID, status, hash[:], []byte("encrypted-private-provider-body"))
		if err != nil {
			t.Fatal(err)
		}
		if tenantID != "" {
			_, err = pool.Exec(ctx, `INSERT INTO outbox_events(id,tenant_id,event_type,aggregate_type,aggregate_id,deduplication_key,payload,status) VALUES($1,$2,'payment.updated','payment',$3,$1,'{}','DEAD')`, deliveryIDs[i], tenantID, "pay_admin_"+suffix)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	repository := New(pool)
	filter := WebhookListFilter{Query: suffix, Limit: 10}
	inbox, err := repository.ListWebhookInboxAdmin(ctx, filter)
	if err != nil || inbox.Total != 3 || len(inbox.Items) != 3 || inbox.Counts["PROCESSED"] != 2 || inbox.Counts["IGNORED"] != 1 {
		t.Fatalf("admin inbox omitted a merchant or legacy event: %#v err=%v", inbox, err)
	}
	seen := map[string]bool{}
	for _, item := range inbox.Items {
		seen[item.TenantID] = true
		if item.CanonicalStatus != "SUCCEEDED" {
			t.Fatalf("inbox lost canonical status: %#v", item)
		}
	}
	if !seen[tenantA] || !seen[tenantB] || !seen[""] {
		t.Fatalf("merchant identities missing: %#v", seen)
	}
	encoded, err := json.Marshal(inbox)
	if err != nil || strings.Contains(string(encoded), "ciphertext") || strings.Contains(string(encoded), "encrypted-private-provider-body") {
		t.Fatalf("inbox response exposes private payload: %s err=%v", encoded, err)
	}
	filtered := filter
	filtered.TenantID = tenantB
	merchantInbox, err := repository.ListWebhookInboxAdmin(ctx, filtered)
	if err != nil || merchantInbox.Total != 1 || len(merchantInbox.Items) != 1 || merchantInbox.Items[0].TenantID != tenantB {
		t.Fatalf("admin merchant filter failed: %#v err=%v", merchantInbox, err)
	}
	// A service caller's actual tenant always wins over a filter claiming another.
	serviceInbox, err := repository.ListWebhookInbox(ctx, tenantA, filtered)
	if err != nil || serviceInbox.Total != 1 || len(serviceInbox.Items) != 1 || serviceInbox.Items[0].TenantID != tenantA {
		t.Fatalf("service inbox escaped tenant scope: %#v err=%v", serviceInbox, err)
	}
	emptyInbox, err := repository.ListWebhookInbox(ctx, "", filter)
	if err != nil || emptyInbox.Total != 0 {
		t.Fatalf("empty service tenant exposes legacy events: %#v err=%v", emptyInbox, err)
	}
	pageFilter := filter
	pageFilter.Status, pageFilter.Limit = "PROCESSED", 1
	first, err := repository.ListWebhookInboxAdmin(ctx, pageFilter)
	if err != nil || first.Total != 2 || !first.HasMore || len(first.Items) != 1 || first.Counts["IGNORED"] != 1 {
		t.Fatalf("admin inbox pagination/counts failed: %#v err=%v", first, err)
	}
	pageFilter.Offset = 1
	second, err := repository.ListWebhookInboxAdmin(ctx, pageFilter)
	if err != nil || second.HasMore || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("admin inbox second page failed: %#v err=%v", second, err)
	}
	deliveries, err := repository.ListWebhookDeliveriesAdmin(ctx, filter)
	if err != nil || deliveries.Total != 2 || len(deliveries.Items) != 2 || deliveries.Counts["DEAD"] != 2 {
		t.Fatalf("admin deliveries failed: %#v err=%v", deliveries, err)
	}
	merchantDeliveries, err := repository.ListWebhookDeliveriesAdmin(ctx, filtered)
	if err != nil || merchantDeliveries.Total != 1 || len(merchantDeliveries.Items) != 1 || merchantDeliveries.Items[0].TenantID != tenantB {
		t.Fatalf("admin delivery merchant filter failed: %#v err=%v", merchantDeliveries, err)
	}
	serviceDeliveries, err := repository.ListWebhookDeliveries(ctx, tenantA, filtered)
	if err != nil || serviceDeliveries.Total != 1 || len(serviceDeliveries.Items) != 1 || serviceDeliveries.Items[0].TenantID != tenantA {
		t.Fatalf("service deliveries escaped tenant scope: %#v err=%v", serviceDeliveries, err)
	}
	emptyDeliveries, err := repository.ListWebhookDeliveries(ctx, "", filter)
	if err != nil || emptyDeliveries.Total != 0 {
		t.Fatalf("empty service tenant exposes deliveries: %#v err=%v", emptyDeliveries, err)
	}
	if _, err = repository.ReplayWebhookDelivery(ctx, tenantA, deliveryIDs[1], "test", "req", "cross-tenant", 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant service replay must fail: %v", err)
	}
	if _, err = repository.ReplayWebhookDeliveryAdmin(ctx, deliveryIDs[1], "payment-proxy-admin", "req", "stale", 1); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("stale admin replay must fail: %v", err)
	}
	replayed, err := repository.ReplayWebhookDeliveryAdmin(ctx, deliveryIDs[1], "payment-proxy-admin", "req", "replay-"+suffix, 0)
	if err != nil || replayed.TenantID != tenantB || replayed.Status != "PENDING" || replayed.ReplayCount != 1 {
		t.Fatalf("admin replay resolved wrong merchant: %#v err=%v", replayed, err)
	}
	duplicate, err := repository.ReplayWebhookDeliveryAdmin(ctx, deliveryIDs[1], "payment-proxy-admin", "req", "replay-"+suffix, 0)
	if err != nil || duplicate.ReplayCount != 1 {
		t.Fatalf("admin replay was not idempotent: %#v err=%v", duplicate, err)
	}
	var auditCount int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE tenant_id=$1 AND resource_id=$2 AND action='webhook.delivery.replay' AND actor='payment-proxy-admin'`, tenantB, deliveryIDs[1]).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("admin replay audit missing/duplicated: count=%d err=%v", auditCount, err)
	}
}
