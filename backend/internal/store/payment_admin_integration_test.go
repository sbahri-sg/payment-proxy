package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdminPaymentProjectionCrossesTenantsWithoutWeakeningServiceScope(t *testing.T) {
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
	tenantA := "itest.payment.admin.a." + suffix
	tenantB := "itest.payment.admin.b." + suffix
	installationA := "ins_itest_admin_a_" + suffix
	installationB := "ins_itest_admin_b_" + suffix
	paymentA := "pay_itest_admin_a_" + suffix
	paymentB := "pay_itest_admin_b_" + suffix
	hashA := sha256.Sum256([]byte(paymentA))
	hashB := sha256.Sum256([]byte(paymentB))

	for _, values := range []struct {
		tenant, installation, payment string
		hash                          []byte
		amount                        int64
	}{
		{tenantA, installationA, paymentA, hashA[:], 10_000},
		{tenantB, installationB, paymentB, hashB[:], 20_000},
	} {
		if _, err = pool.Exec(ctx, `
			INSERT INTO provider_installations(
				id,tenant_id,provider_code,provider_version,environment,engine_profile_id,
				status,created_by,updated_by
			) VALUES($1,$2,'xendit','emisell-xendit-v2.0.1','sandbox','integration','ACTIVE','integration','integration')
		`, values.installation, values.tenant); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `
			INSERT INTO payment_sessions(
				id,tenant_id,installation_id,provider_code,provider_version,environment,merchant_reference,
				idempotency_key,request_hash,amount,currency,status,execution_engine
			) VALUES($1,$2,$3,'xendit','emisell-xendit-v2.0.1','sandbox',$4,$5,$6,$7,'IDR','PENDING','emisell_native')
		`, values.payment, values.tenant, values.installation, "order-"+values.payment, "idem-"+values.payment, values.hash, values.amount); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `
		INSERT INTO payment_status_history(tenant_id,payment_id,status,source,details)
		VALUES($1,$2,'PENDING','integration.test','{"cross_tenant":true}'::jsonb)
	`, tenantB, paymentB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM payment_status_history WHERE payment_id IN ($1,$2)`, paymentA, paymentB)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM payment_sessions WHERE id IN ($1,$2)`, paymentA, paymentB)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_installations WHERE id IN ($1,$2)`, installationA, installationB)
	})

	repository := New(pool)
	serviceList, err := repository.ListPayments(ctx, tenantA, PaymentListFilter{Query: suffix, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if serviceList.Total != 1 || len(serviceList.Items) != 1 || serviceList.Items[0].TenantID != tenantA {
		t.Fatalf("service projection escaped tenant scope: %#v", serviceList)
	}

	adminList, err := repository.ListPaymentsAdmin(ctx, PaymentListFilter{Query: suffix, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if adminList.Total != 2 || len(adminList.Items) != 2 {
		t.Fatalf("admin projection did not include both merchants: %#v", adminList)
	}
	seen := map[string]bool{}
	for _, payment := range adminList.Items {
		seen[payment.TenantID] = true
	}
	if !seen[tenantA] || !seen[tenantB] {
		t.Fatalf("admin projection omitted a merchant: %#v", seen)
	}

	adminPayment, err := repository.GetPaymentAdmin(ctx, paymentB)
	if err != nil || adminPayment.TenantID != tenantB || adminPayment.Amount != 20_000 {
		t.Fatalf("admin payment lookup returned the wrong record: %#v err=%v", adminPayment, err)
	}
	timeline, err := repository.PaymentTimelineAdmin(ctx, paymentB)
	if err != nil || len(timeline) != 1 || timeline[0].PaymentID != paymentB {
		t.Fatalf("admin payment timeline was not merchant-aware: %#v err=%v", timeline, err)
	}
}
