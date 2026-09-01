CREATE TABLE IF NOT EXISTS installation_verifications (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    installation_id text NOT NULL,
    provider_code text NOT NULL,
    provider_version text NOT NULL,
    environment text NOT NULL CHECK (environment IN ('sandbox','live')),
    manifest_digest text NOT NULL DEFAULT ''
        CHECK (manifest_digest = '' OR manifest_digest ~ '^[0-9a-f]{64}$'),
    result text NOT NULL CHECK (result IN ('PASSED','FAILED')),
    connector_id text NOT NULL DEFAULT '',
    webhook_ready boolean NOT NULL DEFAULT false,
    error_code text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    verified_by text NOT NULL,
    request_id text NOT NULL DEFAULT '',
    verified_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, installation_id)
        REFERENCES provider_installations(tenant_id, id) ON DELETE RESTRICT,
    FOREIGN KEY (provider_code, provider_version)
        REFERENCES provider_versions(provider_code, version) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS installation_verifications_installation_idx
    ON installation_verifications(tenant_id, installation_id, verified_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS installation_verifications_runtime_idx
    ON installation_verifications(provider_code, provider_version, result, verified_at DESC);
