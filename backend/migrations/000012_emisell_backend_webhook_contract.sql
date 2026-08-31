-- Upgrade only events that may still be delivered. Historical DELIVERED rows
-- remain byte-for-byte unchanged because their signed payload is immutable.
UPDATE outbox_events
SET payload = jsonb_build_object(
        'id', id,
        'object', 'event',
        'api_version', '2026-08-28',
        'type', event_type,
        'created_at', created_at,
        'merchant_id', tenant_id,
        'resource', jsonb_build_object('type', aggregate_type, 'id', aggregate_id),
        'data', CASE
            WHEN aggregate_type = 'payment' THEN jsonb_build_object(
                'payment', COALESCE(payload->'data', '{}'::jsonb),
                'previous_status', NULL
            )
            WHEN aggregate_type = 'refund' THEN jsonb_build_object(
                'refund', COALESCE(payload->'data', '{}'::jsonb)
            )
            ELSE jsonb_build_object('object', COALESCE(payload->'data', '{}'::jsonb))
        END
    ),
    updated_at = now()
WHERE status IN ('PENDING', 'PROCESSING', 'DEAD')
  AND COALESCE(payload->>'api_version', '') = '';
