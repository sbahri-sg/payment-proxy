ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS flags jsonb NOT NULL DEFAULT '[]'::jsonb;

DO $$
BEGIN
    ALTER TABLE payment_sessions
        ADD CONSTRAINT payment_sessions_flags_array
        CHECK (jsonb_typeof(flags) = 'array');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

COMMENT ON COLUMN payment_sessions.flags IS
    'Canonical, provider-neutral risk and lifecycle markers such as late_payment and provider_delayed_confirmation.';

CREATE TABLE IF NOT EXISTS integration_evidence (
    tenant_id text NOT NULL,
    environment text NOT NULL CHECK (environment IN ('sandbox', 'live')),
    code text NOT NULL,
    observation_count bigint NOT NULL DEFAULT 1 CHECK (observation_count > 0),
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    first_observed_at timestamptz NOT NULL DEFAULT now(),
    last_observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, environment, code),
    CHECK (jsonb_typeof(details) = 'object')
);

COMMENT ON TABLE integration_evidence IS
    'Non-secret evidence used to calculate merchant integration readiness without filling the audit log with read events.';
