CREATE TABLE IF NOT EXISTS providers (
    code text PRIMARY KEY CHECK (code ~ '^[a-z0-9_-]{2,48}$'),
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    available boolean NOT NULL DEFAULT false,
    engine_connector text NOT NULL,
    credential_schema jsonb NOT NULL DEFAULT '[]'::jsonb,
    environments jsonb NOT NULL DEFAULT '["live"]'::jsonb,
    payment_methods jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO providers (
    code, name, description, available, engine_connector,
    credential_schema, environments, payment_methods
) VALUES
(
    'xendit',
    'Xendit',
    'Connector Xendit yang dieksekusi oleh Hyperswitch.',
    true,
    'xendit',
    '[
      {"code":"api_key","label":"Secret API key","input_type":"password","secret":true,"required":true}
    ]'::jsonb,
    '["sandbox","live"]'::jsonb,
    '["qris"]'::jsonb
),
(
    'midtrans',
    'Midtrans',
    'Connector Midtrans direncanakan setelah conformance Xendit selesai.',
    false,
    'midtrans',
    '[
      {"code":"server_key","label":"Server key","input_type":"password","secret":true,"required":true},
      {"code":"client_key","label":"Client key","input_type":"text","secret":false,"required":true}
    ]'::jsonb,
    '["sandbox","live"]'::jsonb,
    '["qris","virtual_account","ewallet","card"]'::jsonb
),
(
    'doku',
    'DOKU',
    'Connector DOKU direncanakan setelah conformance Xendit selesai.',
    false,
    'doku',
    '[
      {"code":"client_id","label":"Client ID","input_type":"text","secret":false,"required":true},
      {"code":"secret_key","label":"Secret key","input_type":"password","secret":true,"required":true}
    ]'::jsonb,
    '["sandbox","live"]'::jsonb,
    '["qris","virtual_account","ewallet","card"]'::jsonb
)
ON CONFLICT (code) DO NOTHING;

CREATE TABLE IF NOT EXISTS provider_versions (
    id text PRIMARY KEY,
    provider_code text NOT NULL REFERENCES providers(code) ON DELETE RESTRICT,
    version text NOT NULL,
    engine_kind text NOT NULL DEFAULT 'hyperswitch_native',
    artifact_digest text NOT NULL DEFAULT '',
    status text NOT NULL CHECK (status IN ('DRAFT','CERTIFIED','RELEASED','SUSPENDED','SUPERSEDED')),
    created_at timestamptz NOT NULL DEFAULT now(),
    released_at timestamptz,
    UNIQUE (provider_code, version)
);

INSERT INTO provider_versions (id, provider_code, version, status, released_at)
VALUES ('pver_xendit_hyperswitch_v1', 'xendit', 'hyperswitch-managed-v1', 'RELEASED', now())
ON CONFLICT (provider_code, version) DO NOTHING;

CREATE TABLE IF NOT EXISTS provider_installations (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    provider_code text NOT NULL REFERENCES providers(code) ON DELETE RESTRICT,
    provider_version text NOT NULL,
    environment text NOT NULL CHECK (environment IN ('sandbox','live')),
    engine_profile_id text NOT NULL,
    engine_connector_id text,
    status text NOT NULL CHECK (status IN (
        'CONFIG_REQUIRED','VERIFYING','READY','ACTIVE','INACTIVE','ERROR','UNINSTALLED'
    )),
    credential_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    payment_methods jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_error text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_by text NOT NULL,
    updated_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    uninstalled_at timestamptz,
    UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS provider_installations_one_current_idx
    ON provider_installations (tenant_id, provider_code, environment)
    WHERE status <> 'UNINSTALLED';
CREATE UNIQUE INDEX IF NOT EXISTS provider_installations_engine_connector_idx
    ON provider_installations (engine_connector_id)
    WHERE engine_connector_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS provider_installations_catalog_idx
    ON provider_installations (tenant_id, environment, status, updated_at DESC);

CREATE TABLE IF NOT EXISTS payment_sessions (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    installation_id text NOT NULL,
    provider_code text NOT NULL,
    environment text NOT NULL CHECK (environment IN ('sandbox','live')),
    merchant_reference text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    amount bigint NOT NULL CHECK (amount > 0),
    currency char(3) NOT NULL,
    status text NOT NULL CHECK (status IN (
        'CREATED','PROCESSING','PENDING','SUCCEEDED','FAILED','CANCELLED','EXPIRED','UNKNOWN'
    )),
    engine_payment_id text,
    connector_transaction_id text NOT NULL DEFAULT '',
    next_action jsonb,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, environment, idempotency_key),
    UNIQUE (tenant_id, environment, merchant_reference),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, installation_id)
        REFERENCES provider_installations(tenant_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS payment_sessions_engine_id_idx
    ON payment_sessions (engine_payment_id)
    WHERE engine_payment_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS payment_sessions_tenant_updated_idx
    ON payment_sessions (tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS refunds (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    payment_id text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    amount bigint NOT NULL CHECK (amount > 0),
    currency char(3) NOT NULL,
    status text NOT NULL CHECK (status IN ('CREATED','PROCESSING','PENDING','SUCCEEDED','FAILED','UNKNOWN')),
    engine_refund_id text,
    last_error text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, idempotency_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, payment_id)
        REFERENCES payment_sessions(tenant_id, id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX IF NOT EXISTS refunds_engine_id_idx
    ON refunds (engine_refund_id)
    WHERE engine_refund_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS webhook_inbox (
    id text PRIMARY KEY,
    source text NOT NULL,
    external_event_id text NOT NULL,
    event_type text NOT NULL,
    payload_sha256 bytea NOT NULL CHECK (octet_length(payload_sha256) = 32),
    payload_ciphertext bytea NOT NULL,
    status text NOT NULL DEFAULT 'PROCESSED' CHECK (status IN ('RECEIVED','PROCESSED','IGNORED','FAILED')),
    error_message text NOT NULL DEFAULT '',
    received_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    UNIQUE (source, external_event_id)
);

CREATE TABLE IF NOT EXISTS outbox_events (
    id text PRIMARY KEY,
    tenant_id text NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    deduplication_key text NOT NULL UNIQUE,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PROCESSING','DELIVERED','DEAD')),
    attempt_count integer NOT NULL DEFAULT 0,
    max_attempts integer NOT NULL DEFAULT 8,
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    locked_by text,
    last_http_status integer,
    last_error text NOT NULL DEFAULT '',
    delivered_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS outbox_events_claim_idx
    ON outbox_events (status, available_at, created_at);

CREATE TABLE IF NOT EXISTS audit_logs (
    id bigserial PRIMARY KEY,
    tenant_id text NOT NULL DEFAULT '',
    actor text NOT NULL,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    request_id text NOT NULL DEFAULT '',
    details jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_logs_tenant_created_idx
    ON audit_logs (tenant_id, created_at DESC);
