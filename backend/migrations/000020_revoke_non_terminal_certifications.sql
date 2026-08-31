UPDATE provider_payment_method_capabilities AS capability
SET support_status = 'DOCUMENTED',
    metadata = capability.metadata - 'certification_run_id',
    updated_at = now()
WHERE capability.support_status = 'CERTIFIED'
  AND EXISTS (
      SELECT 1
      FROM connector_certification_runs AS run
      WHERE run.id = capability.metadata ->> 'certification_run_id'
        AND run.status = 'PASSED'
        AND (
            NOT EXISTS (
                SELECT 1
                FROM webhook_inbox AS inbox
                WHERE inbox.tenant_id = run.tenant_id
                  AND inbox.source = run.provider_code
                  AND inbox.aggregate_type = 'payment'
                  AND inbox.aggregate_id = run.payment_id
                  AND inbox.status = 'PROCESSED'
                  AND inbox.canonical_status = 'SUCCEEDED'
            )
            OR NOT EXISTS (
                SELECT 1
                FROM outbox_events AS outbox
                WHERE outbox.tenant_id = run.tenant_id
                  AND outbox.aggregate_type = 'payment'
                  AND outbox.aggregate_id = run.payment_id
                  AND outbox.event_type = 'payment.updated'
                  AND outbox.status = 'DELIVERED'
                  AND outbox.payload #>> '{data,payment,status}' = 'SUCCEEDED'
            )
        )
  );
UPDATE connector_certification_runs AS run
SET status = 'BLOCKED',
    checks = run.checks || jsonb_build_array(jsonb_build_object(
        'code', 'terminal_webhook_evidence',
        'label', 'Terminal provider webhook and Emisell delivery',
        'status', 'BLOCKED',
        'detail', 'A non-terminal webhook cannot certify a successful payment.'
    )),
    message = 'Certification revoked: a terminal SUCCEEDED provider webhook and matching Emisell delivery are required.',
    completed_at = now()
WHERE run.status = 'PASSED'
  AND (
      NOT EXISTS (
          SELECT 1
          FROM webhook_inbox AS inbox
          WHERE inbox.tenant_id = run.tenant_id
            AND inbox.source = run.provider_code
            AND inbox.aggregate_type = 'payment'
            AND inbox.aggregate_id = run.payment_id
            AND inbox.status = 'PROCESSED'
            AND inbox.canonical_status = 'SUCCEEDED'
      )
      OR NOT EXISTS (
          SELECT 1
          FROM outbox_events AS outbox
          WHERE outbox.tenant_id = run.tenant_id
            AND outbox.aggregate_type = 'payment'
            AND outbox.aggregate_id = run.payment_id
            AND outbox.event_type = 'payment.updated'
            AND outbox.status = 'DELIVERED'
            AND outbox.payload #>> '{data,payment,status}' = 'SUCCEEDED'
      )
  );
