UPDATE providers
SET payment_methods='["qris"]'::jsonb,
    updated_at=now()
WHERE code='xendit';

UPDATE provider_installations
SET credential_metadata=jsonb_set(
    credential_metadata,
    '{configured_fields}',
    COALESCE(
        (
            SELECT jsonb_agg(field - 'last_four')
            FROM jsonb_array_elements(credential_metadata->'configured_fields') AS field
        ),
        '[]'::jsonb
    )
)
WHERE jsonb_typeof(credential_metadata->'configured_fields')='array';
