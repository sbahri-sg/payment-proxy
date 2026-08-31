CREATE TABLE IF NOT EXISTS provider_credentials (
    installation_id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) > 28),
    key_version integer NOT NULL DEFAULT 1 CHECK (key_version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, installation_id),
    FOREIGN KEY (tenant_id, installation_id)
        REFERENCES provider_installations(tenant_id, id) ON DELETE CASCADE
);

ALTER TABLE provider_installations
    ADD COLUMN IF NOT EXISTS execution_engine text NOT NULL DEFAULT 'emisell_native'
        CHECK (execution_engine IN ('emisell_native','legacy_external'));

ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS execution_engine text NOT NULL DEFAULT 'legacy_external'
        CHECK (execution_engine IN ('emisell_native','legacy_external'));

ALTER TABLE refunds
    ADD COLUMN IF NOT EXISTS execution_engine text NOT NULL DEFAULT 'legacy_external'
        CHECK (execution_engine IN ('emisell_native','legacy_external'));

INSERT INTO provider_versions (id,provider_code,version,engine_kind,status,released_at)
VALUES ('pver_xendit_emisell_native_v1','xendit','emisell-xendit-v1','emisell_native','RELEASED',now())
ON CONFLICT (provider_code,version) DO UPDATE SET
    engine_kind='emisell_native',status='RELEASED',released_at=COALESCE(provider_versions.released_at,now());

UPDATE provider_versions
SET status='SUPERSEDED'
WHERE provider_code='xendit' AND version='hyperswitch-managed-v1';

UPDATE providers
SET description='Connector Xendit native yang dijalankan oleh Emisell Payment Engine.',
    engine_connector='xendit',
    credential_schema='[
      {"code":"api_key","label":"Secret API key","input_type":"password","secret":true,"required":true},
      {"code":"webhook_verification_token","label":"Webhook verification token (optional)","input_type":"password","secret":true,"required":false}
    ]'::jsonb,
    available=true,
    updated_at=now()
WHERE code='xendit';

UPDATE provider_payment_method_capabilities
SET provider_method='qr_code',provider_method_type='qris',provider_channel_code='QRIS',
    support_status='CERTIFIED',
    source_url='https://docs.xendit.co/docs/qris',
    metadata='{"engine":"emisell_native","provider_api":"payments_v3","conformance_profile":"xendit-v3-qris"}'::jsonb,
    updated_at=now()
WHERE provider_code='xendit' AND payment_method_code='qris';

UPDATE provider_payment_method_capabilities
SET provider_method='bank_transfer',provider_method_type='bca',provider_channel_code='BCA_VIRTUAL_ACCOUNT',
    support_status='CERTIFIED',
    source_url='https://docs.xendit.co/docs/bca-virtual-account',
    metadata='{"engine":"emisell_native","provider_api":"payments_v3","conformance_profile":"xendit-v3-bca-va"}'::jsonb,
    updated_at=now()
WHERE provider_code='xendit' AND payment_method_code='va_bca';

UPDATE provider_installations
SET provider_version='emisell-xendit-v1',
    engine_profile_id='emisell-native',
    engine_connector_id=NULL,
    execution_engine='emisell_native',
    status='CONFIG_REQUIRED',
    credential_metadata='{}'::jsonb,
    payment_methods='[]'::jsonb,
    last_error='',
    updated_by='migration.emisell-native',
    updated_at=now(),
    version=version+1
WHERE provider_code='xendit' AND status <> 'UNINSTALLED';
