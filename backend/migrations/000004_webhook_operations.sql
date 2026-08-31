ALTER TABLE webhook_inbox
    ADD COLUMN IF NOT EXISTS tenant_id text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS aggregate_type text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS aggregate_id text NOT NULL DEFAULT '';

UPDATE webhook_inbox AS inbox
SET tenant_id=outbox.tenant_id,
    aggregate_type=outbox.aggregate_type,
    aggregate_id=outbox.aggregate_id
FROM outbox_events AS outbox
WHERE outbox.deduplication_key='hyperswitch:' || inbox.external_event_id
  AND inbox.tenant_id='';

CREATE INDEX IF NOT EXISTS webhook_inbox_tenant_received_idx
    ON webhook_inbox (tenant_id, received_at DESC)
    WHERE tenant_id<>'';

ALTER TABLE outbox_events
    ADD COLUMN IF NOT EXISTS replay_count integer NOT NULL DEFAULT 0 CHECK (replay_count >= 0),
    ADD COLUMN IF NOT EXISTS last_replayed_at timestamptz,
    ADD COLUMN IF NOT EXISTS last_replayed_by text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_replay_key text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS outbox_events_tenant_updated_idx
    ON outbox_events (tenant_id, updated_at DESC);
