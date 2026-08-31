CREATE TABLE IF NOT EXISTS payment_method_assignments (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    environment text NOT NULL CHECK (environment IN ('sandbox','live')),
    payment_method text NOT NULL CHECK (payment_method ~ '^[a-z0-9_]{2,64}$'),
    payment_method_type text NOT NULL CHECK (payment_method_type ~ '^[a-z0-9_]{2,64}$'),
    installation_id text NOT NULL,
    label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 96),
    status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','INACTIVE')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by text NOT NULL,
    updated_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, environment, payment_method, payment_method_type),
    FOREIGN KEY (tenant_id, installation_id)
        REFERENCES provider_installations(tenant_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS payment_method_assignments_tenant_environment_idx
    ON payment_method_assignments (tenant_id, environment, status, updated_at DESC);

ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS payment_option_id text
        REFERENCES payment_method_assignments(id) ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS payment_sessions_tenant_option_idx
    ON payment_sessions (tenant_id, payment_option_id)
    WHERE payment_option_id IS NOT NULL;
