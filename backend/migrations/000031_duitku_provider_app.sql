-- Duitku now has a real shared Provider App runtime. Availability remains
-- release-driven: the provider becomes installable only after v1.0.0 passes
-- runtime verification and is published through the release registry.
UPDATE providers
SET description = 'Connector Duitku POP untuk hosted checkout resmi, pembayaran langsung per channel, status transaksi, dan callback terverifikasi.',
    engine_connector = 'duitku',
    credential_schema = '[{"code":"merchant_code","label":"Merchant code","input_type":"text","secret":false,"required":true},{"code":"api_key","label":"API key","input_type":"password","secret":true,"required":true}]'::jsonb,
    environments = '["sandbox","live"]'::jsonb,
    updated_at = now()
WHERE code = 'duitku';
