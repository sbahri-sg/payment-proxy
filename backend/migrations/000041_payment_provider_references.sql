CREATE TABLE IF NOT EXISTS payment_provider_references (
    provider_code text NOT NULL,
    installation_id text NOT NULL,
    provider_reference text NOT NULL,
    tenant_id text NOT NULL,
    payment_id text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_code, installation_id, provider_reference),
    FOREIGN KEY (tenant_id, payment_id)
        REFERENCES payment_sessions(tenant_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS payment_provider_references_payment_idx
    ON payment_provider_references (tenant_id, payment_id);

INSERT INTO payment_provider_references(
    provider_code, installation_id, provider_reference, tenant_id, payment_id
)
SELECT DISTINCT
    payment.provider_code,
    payment.installation_id,
    reference.value,
    payment.tenant_id,
    payment.id
FROM payment_sessions AS payment
CROSS JOIN LATERAL unnest(ARRAY[
    NULLIF(payment.engine_payment_id, ''),
    NULLIF(payment.connector_transaction_id, '')
]) AS reference(value)
WHERE reference.value IS NOT NULL
ON CONFLICT (provider_code, installation_id, provider_reference) DO NOTHING;

COMMENT ON TABLE payment_provider_references IS
    'Provider-generated aliases used to correlate webhooks with canonical payment sessions.';
