ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS provider_version text;

UPDATE payment_sessions payment
SET provider_version=installation.provider_version
FROM provider_installations installation
WHERE installation.tenant_id=payment.tenant_id
  AND installation.id=payment.installation_id
  AND (payment.provider_version IS NULL OR payment.provider_version='');

ALTER TABLE payment_sessions
    ALTER COLUMN provider_version SET NOT NULL,
    ADD CONSTRAINT payment_sessions_provider_version_required
        CHECK (btrim(provider_version) <> ''),
    ADD CONSTRAINT payment_sessions_runtime_version_fkey
        FOREIGN KEY(provider_code,provider_version)
        REFERENCES provider_versions(provider_code,version)
        ON DELETE RESTRICT;

ALTER TABLE provider_installations
    ADD CONSTRAINT provider_installations_runtime_version_fkey
        FOREIGN KEY(provider_code,provider_version)
        REFERENCES provider_versions(provider_code,version)
        ON DELETE RESTRICT;

CREATE INDEX IF NOT EXISTS payment_sessions_runtime_version_idx
    ON payment_sessions(provider_code,provider_version,updated_at DESC);
