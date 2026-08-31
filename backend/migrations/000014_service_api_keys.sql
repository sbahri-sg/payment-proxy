CREATE TABLE IF NOT EXISTS service_api_keys (
    id text PRIMARY KEY,
    name text NOT NULL CHECK (char_length(name) BETWEEN 3 AND 80),
    key_prefix text NOT NULL,
    key_last_four char(4) NOT NULL,
    key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
    scopes text[] NOT NULL DEFAULT ARRAY['gateway:full']::text[],
    status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','REVOKED')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_by text,
    revoked_at timestamptz,
    UNIQUE (key_hash)
);

CREATE INDEX IF NOT EXISTS service_api_keys_status_created_idx
    ON service_api_keys (status, created_at DESC);

COMMENT ON TABLE service_api_keys IS
    'One-time-display API keys used by Emisell Backend to access the full /api/v1 gateway.';
COMMENT ON COLUMN service_api_keys.key_hash IS
    'SHA-256 digest of the API key. Plaintext is never persisted.';
