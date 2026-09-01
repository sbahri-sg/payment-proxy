-- Runtime availability is operational state, not merchant configuration.
-- Keep it in a short-lived internal cache so an outage can hide a checkout
-- option without changing the merchant's ACTIVE/INACTIVE assignment.

CREATE TABLE IF NOT EXISTS installation_payment_method_availability (
    tenant_id text NOT NULL,
    installation_id text NOT NULL,
    payment_method_code text NOT NULL CHECK (
        payment_method_code = '*' OR payment_method_code ~ '^[a-z0-9_]{2,64}$'
    ),
    status text NOT NULL CHECK (status IN ('AVAILABLE','UNAVAILABLE')),
    reason text NOT NULL DEFAULT '',
    source text NOT NULL DEFAULT 'installation_verification',
    checked_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, installation_id, payment_method_code),
    FOREIGN KEY (tenant_id, installation_id)
        REFERENCES provider_installations(tenant_id, id) ON DELETE CASCADE,
    CHECK (expires_at > checked_at)
);

CREATE INDEX IF NOT EXISTS installation_payment_method_availability_expiry_idx
    ON installation_payment_method_availability (expires_at);
