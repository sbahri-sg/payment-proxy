CREATE TABLE IF NOT EXISTS connector_certification_runs (
    id text PRIMARY KEY,
    tenant_id text NOT NULL CHECK (tenant_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    installation_id text NOT NULL,
    provider_code text NOT NULL,
    payment_method_code text NOT NULL,
    environment text NOT NULL CHECK (environment IN ('sandbox','live')),
    status text NOT NULL CHECK (status IN ('PASSED','FAILED','BLOCKED')),
    checks jsonb NOT NULL DEFAULT '[]'::jsonb,
    payment_id text,
    message text NOT NULL DEFAULT '',
    initiated_by text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id,installation_id)
        REFERENCES provider_installations(tenant_id,id) ON DELETE RESTRICT,
    FOREIGN KEY (provider_code,payment_method_code)
        REFERENCES provider_payment_method_capabilities(provider_code,payment_method_code) ON DELETE RESTRICT,
    FOREIGN KEY (tenant_id,payment_id)
        REFERENCES payment_sessions(tenant_id,id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS connector_certification_runs_tenant_idx
    ON connector_certification_runs (tenant_id,environment,completed_at DESC);

-- Hyperswitch classic v1 names this canonical type `bca_bank_transfer`.
-- Xendit's current built-in Hyperswitch connector does not implement that flow,
-- so it remains documented and cannot be assigned to checkout.
UPDATE provider_payment_method_capabilities
SET provider_method='bank_transfer',
    provider_method_type='bca_bank_transfer',
    provider_channel_code='BCA_VIRTUAL_ACCOUNT',
    source_url='https://docs.xendit.co/docs/bca-virtual-account',
    metadata=jsonb_build_object(
        'engine','hyperswitch',
        'engine_support','UNSUPPORTED',
        'blocker_code','HYPERSWITCH_XENDIT_METHOD_NOT_IMPLEMENTED',
        'recommended_path','partner_connector',
        'provider_api','POST /v3/payment_requests',
        'provider_channel_code','BCA_VIRTUAL_ACCOUNT'
    ),
    support_status='DOCUMENTED',
    updated_at=now()
WHERE provider_code='xendit' AND payment_method_code='va_bca';

UPDATE provider_payment_method_capabilities
SET metadata=jsonb_build_object(
        'engine','hyperswitch',
        'engine_support','SUPPORTED',
        'conformance_profile','qris-webhook',
        'evidence','create + next action + provider simulation + signed webhook outbox'
    ),
    updated_at=now()
WHERE provider_code='xendit' AND payment_method_code='qris';
