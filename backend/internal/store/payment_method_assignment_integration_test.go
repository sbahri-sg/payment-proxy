package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/emisell/api-payment-proxy/internal/connector"
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
	liveInstallationID := "ins_assignment_live_" + suffix
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
		) VALUES
		($1,$2,'xendit',$3,'sandbox','integration',NULL,'ACTIVE','{}'::jsonb,'integration','integration'),
		($4,$2,'xendit',$3,'live','integration',NULL,'ACTIVE','{}'::jsonb,'integration','integration')
	`, installationID, tenantID, providerVersion, liveInstallationID)
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
	if _, _, err = repository.UpsertPaymentMethodAssignments(ctx, []UpsertPaymentMethodAssignmentInput{
		{ID: "pmo_card_live_" + suffix, TenantID: tenantID, Environment: model.EnvironmentLive, PaymentMethodCode: "card", PaymentMethod: "card", PaymentMethodType: "card", InstallationID: liveInstallationID, ExpectedVersion: 0, Actor: "integration", RequestID: "req-live-create"},
	}); err != nil {
		t.Fatal(err)
	}
	listed, err := repository.ListPaymentMethodAssignments(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]string, len(listed))
	environments := make(map[string]string, len(listed))
	for _, item := range listed {
		statuses[item.PaymentMethodCode] = item.Status
		environments[item.PaymentMethodCode] = item.Environment
	}
	if statuses["qris"] != model.PaymentMethodAssignmentInactive || statuses["va_bca"] != model.PaymentMethodAssignmentActive {
		t.Fatalf("list omitted an assignment status: %#v", statuses)
	}
	if statuses["card"] != model.PaymentMethodAssignmentActive || environments["card"] != model.EnvironmentLive || environments["qris"] != model.EnvironmentSandbox {
		t.Fatalf("merchant assignment list did not include both environments: statuses=%#v environments=%#v", statuses, environments)
	}
	paymentOptions, err := repository.ListPaymentOptions(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	optionEnvironments := make(map[string]string, len(paymentOptions))
	for _, item := range paymentOptions {
		optionEnvironments[item.PaymentMethodCode] = item.Environment
	}
	if len(paymentOptions) != 2 || optionEnvironments["va_bca"] != model.EnvironmentSandbox || optionEnvironments["card"] != model.EnvironmentLive {
		t.Fatalf("active payment options did not include both environments: %#v", paymentOptions)
	}
	if _, exists := optionEnvironments["qris"]; exists {
		t.Fatalf("inactive payment option leaked into checkout list: %#v", paymentOptions)
	}
	activeMappings, err := repository.ListActivePaymentMethodMappings(ctx, tenantID, installationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(activeMappings) != 1 || activeMappings[0].PaymentMethodCode != "va_bca" || activeMappings[0].ProviderChannelCode != "BCA_VIRTUAL_ACCOUNT" {
		t.Fatalf("active hosted-checkout mappings were not isolated: %#v", activeMappings)
	}
	otherTenantMappings, err := repository.ListActivePaymentMethodMappings(ctx, "other."+tenantID, installationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTenantMappings) != 0 {
		t.Fatalf("another tenant could read hosted-checkout mappings: %#v", otherTenantMappings)
	}
	providerOptions, err := repository.ListProviderOptions(ctx, tenantID, model.EnvironmentSandbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerOptions) != 1 || providerOptions[0].ProviderCode != "xendit" || providerOptions[0].InstallationID != installationID {
		t.Fatalf("provider options were not grouped by active installation: %#v", providerOptions)
	}
	if len(providerOptions[0].SupportedPaymentMethods) != 1 || providerOptions[0].SupportedPaymentMethods[0].PaymentMethodCode != "va_bca" {
		t.Fatalf("inactive method leaked into provider options: %#v", providerOptions[0].SupportedPaymentMethods)
	}
	otherTenantOptions, err := repository.ListProviderOptions(ctx, "other."+tenantID, model.EnvironmentSandbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherTenantOptions) != 0 {
		t.Fatalf("another tenant could read provider options: %#v", otherTenantOptions)
	}

	checkedAt := time.Now().UTC()
	if err = repository.ReplaceInstallationAvailability(ctx, tenantID, installationID, InstallationAvailabilitySnapshot{
		ProviderStatus: connector.PaymentMethodAvailabilityAvailable,
		Methods: []connector.PaymentMethodAvailability{
			{PaymentMethodCode: "va_bca", Status: connector.PaymentMethodAvailabilityUnavailable, Reason: "PROVIDER_MAINTENANCE"},
		},
		CheckedAt: checkedAt,
		ExpiresAt: checkedAt.Add(2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if available, availabilityErr := repository.IsPaymentMethodAvailable(ctx, tenantID, installationID, "va_bca", time.Now()); availabilityErr != nil || available {
		t.Fatalf("maintenance method remained available: available=%v err=%v", available, availabilityErr)
	}
	availabilityOverview, err := repository.ListProviderAvailabilityAdmin(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var operationalItem *model.ProviderAvailabilityItem
	for index := range availabilityOverview.Items {
		if availabilityOverview.Items[index].MerchantID == tenantID && availabilityOverview.Items[index].InstallationID == installationID {
			operationalItem = &availabilityOverview.Items[index]
			break
		}
	}
	if operationalItem == nil || operationalItem.Status != "DEGRADED" || len(operationalItem.UnavailablePaymentMethods) != 1 || !operationalItem.UnavailablePaymentMethods[0].Fresh {
		t.Fatalf("admin availability did not expose the affected merchant connection: %#v", operationalItem)
	}
	assignmentsDuringMaintenance, err := repository.ListPaymentMethodAssignments(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	assignmentStatuses := make(map[string]string, len(assignmentsDuringMaintenance))
	for _, item := range assignmentsDuringMaintenance {
		assignmentStatuses[item.PaymentMethodCode] = item.Status
	}
	if assignmentStatuses["va_bca"] != model.PaymentMethodAssignmentActive {
		t.Fatalf("runtime outage changed merchant assignment: %#v", assignmentStatuses)
	}
	optionsDuringMaintenance, err := repository.ListPaymentOptions(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(optionsDuringMaintenance) != 1 || optionsDuringMaintenance[0].PaymentMethodCode != "card" {
		t.Fatalf("maintenance method leaked into checkout options: %#v", optionsDuringMaintenance)
	}
	providerOptionsDuringMaintenance, err := repository.ListProviderOptions(ctx, tenantID, model.EnvironmentSandbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(providerOptionsDuringMaintenance) != 0 {
		t.Fatalf("provider with no available assigned method leaked into checkout: %#v", providerOptionsDuringMaintenance)
	}
	mappingsDuringMaintenance, err := repository.ListActivePaymentMethodMappings(ctx, tenantID, installationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappingsDuringMaintenance) != 0 {
		t.Fatalf("maintenance method leaked into hosted allowlist: %#v", mappingsDuringMaintenance)
	}
	if _, err = repository.GetActivePaymentOption(ctx, tenantID, model.EnvironmentSandbox, inputs[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("maintenance payment option remained selectable: %v", err)
	}
	if _, err = pool.Exec(ctx, `
		UPDATE installation_payment_method_availability
		SET checked_at=now()-interval '2 minutes', expires_at=now()-interval '1 minute'
		WHERE tenant_id=$1 AND installation_id=$2
	`, tenantID, installationID); err != nil {
		t.Fatal(err)
	}
	optionsAfterExpiry, err := repository.ListPaymentOptions(ctx, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(optionsAfterExpiry) != 2 {
		t.Fatalf("expired outage cache still hid checkout option: %#v", optionsAfterExpiry)
	}
	availabilityOverview, err = repository.ListProviderAvailabilityAdmin(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	operationalItem = nil
	for index := range availabilityOverview.Items {
		if availabilityOverview.Items[index].MerchantID == tenantID && availabilityOverview.Items[index].InstallationID == installationID {
			operationalItem = &availabilityOverview.Items[index]
			break
		}
	}
	if operationalItem == nil || operationalItem.Status != "UNKNOWN" || len(operationalItem.UnavailablePaymentMethods) != 1 || operationalItem.UnavailablePaymentMethods[0].Fresh {
		t.Fatalf("expired evidence was presented as a current incident: %#v", operationalItem)
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
