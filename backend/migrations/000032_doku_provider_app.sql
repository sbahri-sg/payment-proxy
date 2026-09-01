-- DOKU availability remains release-driven. Publishing the verified shared
-- runtime makes the provider installable; this migration only defines its
-- merchant-facing connection contract.
UPDATE providers
SET description = 'Connector DOKU Checkout untuk payment link resmi, pembayaran langsung per channel, status order, dan notification HMAC terverifikasi.',
    engine_connector = 'doku',
    credential_schema = '[{"code":"client_id","label":"Client ID","input_type":"text","secret":false,"required":true},{"code":"secret_key","label":"Secret key","input_type":"password","secret":true,"required":true}]'::jsonb,
    environments = '["sandbox","live"]'::jsonb,
    updated_at = now()
WHERE code = 'doku';
