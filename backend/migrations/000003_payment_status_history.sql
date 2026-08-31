CREATE TABLE IF NOT EXISTS payment_status_history (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL,
    payment_id text NOT NULL,
    status text NOT NULL CHECK (status IN (
        'CREATED','PROCESSING','PENDING','SUCCEEDED','FAILED','CANCELLED','EXPIRED','UNKNOWN'
    )),
    source text NOT NULL,
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, payment_id)
        REFERENCES payment_sessions(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS payment_status_history_tenant_payment_created_idx
    ON payment_status_history (tenant_id, payment_id, created_at, id);

INSERT INTO payment_status_history (tenant_id, payment_id, status, source, details, created_at)
SELECT tenant_id, id, status, 'migration.backfill', '{"backfilled":true}'::jsonb, created_at
FROM payment_sessions p
WHERE NOT EXISTS (
    SELECT 1 FROM payment_status_history h
    WHERE h.tenant_id=p.tenant_id AND h.payment_id=p.id
);
