ALTER TABLE webhook_inbox
    ADD COLUMN IF NOT EXISTS canonical_status text NOT NULL DEFAULT '';

UPDATE webhook_inbox AS inbox
SET canonical_status = CASE
    WHEN inbox.aggregate_type = 'payment' THEN COALESCE(outbox.payload #>> '{data,payment,status}', '')
    WHEN inbox.aggregate_type = 'refund' THEN COALESCE(outbox.payload #>> '{data,refund,status}', '')
    ELSE ''
END
FROM outbox_events AS outbox
WHERE outbox.deduplication_key = inbox.source || ':' || inbox.external_event_id
  AND inbox.canonical_status = '';

CREATE INDEX IF NOT EXISTS webhook_inbox_certification_evidence_idx
    ON webhook_inbox (tenant_id, source, aggregate_type, aggregate_id, canonical_status)
    WHERE status = 'PROCESSED';
