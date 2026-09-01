ALTER TABLE payment_sessions
    ADD COLUMN IF NOT EXISTS payment_method_code text
        REFERENCES payment_methods(code) ON DELETE RESTRICT;

UPDATE payment_sessions payment
SET payment_method_code=assignment.payment_method_code
FROM payment_method_assignments assignment
WHERE assignment.tenant_id=payment.tenant_id
  AND assignment.id=payment.payment_option_id
  AND payment.payment_method_code IS NULL;

CREATE INDEX IF NOT EXISTS payment_sessions_method_idx
    ON payment_sessions(tenant_id,payment_method_code,updated_at DESC);

ALTER TABLE refunds
    ADD COLUMN IF NOT EXISTS payment_method_code text
        REFERENCES payment_methods(code) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS reason text NOT NULL DEFAULT 'OTHERS'
        CHECK (reason IN ('REQUESTED_BY_CUSTOMER','CANCELLATION','DUPLICATE','FRAUDULENT','OTHERS')),
    ADD COLUMN IF NOT EXISTS requested_by text NOT NULL DEFAULT 'migration';

UPDATE refunds refund
SET payment_method_code=payment.payment_method_code
FROM payment_sessions payment
WHERE payment.tenant_id=refund.tenant_id
  AND payment.id=refund.payment_id
  AND refund.payment_method_code IS NULL;

-- Refund is fail-closed per payment channel. A connector-level operation is
-- necessary but not sufficient: the original payment channel must explicitly
-- advertise a return-to-source policy as well.
UPDATE provider_payment_method_capabilities
SET metadata=metadata || jsonb_build_object('refund',jsonb_build_object(
        'supported',true,
        'partial',false,
        'multiple_partial',false,
        'return_to_original_source',true,
        'confirmation','webhook',
        'window_days',30,
        'source_url','https://docs.xendit.co/docs/en/available-payment-channels'
    )),
    updated_at=now()
WHERE provider_code='xendit' AND payment_method_code='qris';

-- Midtrans remains unconfigured here. Its adapter is not enough evidence to
-- publish channel policy; capability metadata is added only together with
-- provider-specific sandbox and webhook certification.
