ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS reconciliation_count integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_reconciled_at timestamptz,
    ADD COLUMN IF NOT EXISTS last_reconciled_by text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_reconciliation_key text NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS emisell_received_events (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    source_timestamp bigint NOT NULL,
    signature_version text NOT NULL DEFAULT 'v1',
    duplicate_count integer NOT NULL DEFAULT 0,
    received_at timestamptz NOT NULL DEFAULT now(),
    last_received_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS emisell_received_events_tenant_received_idx
    ON emisell_received_events (tenant_id, received_at DESC);
