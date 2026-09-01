package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBulkPaymentMethodAssignmentsAreAtomicAndListAllStatuses(t *testing.T) {
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
	tenantID := "itest.assignment." + suffix
	installationID := "ins_assignment_" + suffix
	var providerVersion string
	if err = pool.QueryRow(ctx, `
		SELECT version FROM provider_versions
		WHERE provider_code='xendit' AND status='RELEASED'
		ORDER BY released_at DESC NULLS LAST LIMIT 1
	`).Scan(&providerVersion); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM audit_logs WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM payment_method_assignments WHERE tenant_id=$1`, tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM provider_installations WHERE tenant_id=$1`, tenantID)
	})
	_, err = pool.Exec(ctx, `
		INSERT INTO provider_installations(
			id,tenant_id,provider_code,provider_version,environment,engine_profile_id,
			engine_connector_id,status,credential_metadata,created_by,updated_by
		) VALUES($1,$2,'xendit',$3,'sandbox','integration',
		         NULL,'ACTIVE','{}'::jsonb,'integration','integration')
	`, installationID, tenantID, providerVersion)
	if err != nil {
		t.Fatal(err)
	}

	repository := New(pool)
	inputs := []UpsertPaymentMethodAssignmentInput{
		{ID: "pmo_va_" + suffix, TenantID: tenantID, Environment: model.EnvironmentSandbox, PaymentMethodCode: "va_bca", PaymentMethod: "bank_transfer", PaymentMethodType: "bca", InstallationID: installationID, ExpectedVersion: 0, Actor: "integration", RequestID: "req-bulk-create"},
		{ID: "pmo_qris_" + suffix, TenantID: tenantID, Environment: model.EnvironmentSandbox, PaymentMethodCode: "qris", PaymentMethod: "real_time_payment", PaymentMethodType: "qris", InstallationID: installationID, ExpectedVersion: 0, Actor: "integration", RequestID: "req-bulk-create"},
	}
	items, created, err := repository.UpsertPaymentMethodAssignments(ctx, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 || len(items) != 2 || items[0].PaymentMethodCode != "va_bca" || items[1].PaymentMethodCode != "qris" {
		t.Fatalf("bulk response did not preserve request order: created=%d items=%#v", created, items)
	}
	qris, err := repository.DeactivatePaymentMethodAssignment(ctx, tenantID, items[1].ID, "integration", "req-deactivate", items[1].Version)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := repository.ListPaymentMethodAssignments(ctx, tenantID, model.EnvironmentSandbox)
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]string, len(listed))
	for _, item := range listed {
		statuses[item.PaymentMethodCode] = item.Status
	}
	if statuses["qris"] != model.PaymentMethodAssignmentInactive || statuses["va_bca"] != model.PaymentMethodAssignmentActive {
		t.Fatalf("list omitted an assignment status: %#v", statuses)
	}

	_, _, err = repository.UpsertPaymentMethodAssignments(ctx, []UpsertPaymentMethodAssignmentInput{
		{ID: "unused_qris_" + suffix, TenantID: tenantID, Environment: model.EnvironmentSandbox, PaymentMethodCode: "qris", PaymentMethod: "real_time_payment", PaymentMethodType: "qris", InstallationID: installationID, ExpectedVersion: qris.Version, Actor: "integration", RequestID: "req-rollback"},
		{ID: "unused_va_" + suffix, TenantID: tenantID, Environment: model.EnvironmentSandbox, PaymentMethodCode: "va_bca", PaymentMethod: "bank_transfer", PaymentMethodType: "bca", InstallationID: installationID, ExpectedVersion: 999, Actor: "integration", RequestID: "req-rollback"},
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("bulk version conflict returned %v", err)
	}
	qrisAfterRollback, err := repository.GetPaymentMethodAssignment(ctx, tenantID, qris.ID)
	if err != nil {
		t.Fatal(err)
	}
	if qrisAfterRollback.Status != model.PaymentMethodAssignmentInactive || qrisAfterRollback.Version != qris.Version {
		t.Fatalf("failed batch was partially committed: before=%#v after=%#v", qris, qrisAfterRollback)
	}
}
