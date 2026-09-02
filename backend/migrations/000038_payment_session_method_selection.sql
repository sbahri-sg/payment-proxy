ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS payment_method_id text
        REFERENCES payment_method_assignments(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS payment_sessions_tenant_method_selection_idx
    ON payment_sessions (tenant_id, payment_method_id)
    WHERE payment_method_id IS NOT NULL;
