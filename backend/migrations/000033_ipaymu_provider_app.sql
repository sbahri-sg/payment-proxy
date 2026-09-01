-- iPaymu availability remains release-driven. This migration adds the provider
-- identity, merchant credential contract, and documented method mappings; the
-- provider becomes installable only after its verified shared runtime release
-- is published.
INSERT INTO providers (
    code,name,description,available,engine_connector,credential_schema,environments,payment_methods
) VALUES (
    'ipaymu','iPaymu',
    'Connector iPaymu API v2 untuk payment link resmi, direct payment per channel, status transaksi, dan callback HMAC terverifikasi.',
    false,'ipaymu',
    '[{"code":"va","label":"VA number","input_type":"text","secret":false,"required":true},{"code":"api_key","label":"API key","input_type":"password","secret":true,"required":true}]'::jsonb,
    '["sandbox","live"]'::jsonb,'[]'::jsonb
)
ON CONFLICT (code) DO UPDATE SET
    name=EXCLUDED.name,
    description=EXCLUDED.description,
    engine_connector=EXCLUDED.engine_connector,
    credential_schema=EXCLUDED.credential_schema,
    environments=EXCLUDED.environments,
    updated_at=now();

INSERT INTO provider_payment_method_capabilities
    (provider_code,payment_method_code,provider_method,provider_method_type,provider_channel_code,support_status,source_url,metadata)
VALUES
('ipaymu','qris','real_time_payment','qris','mpm','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','card','card','card','cc','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_arta_graha','bank_transfer','arta_graha','bag','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_bca','bank_transfer','bca','bca','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_bni','bank_transfer','bni','bni','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_cimb','bank_transfer','cimb','cimb','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_mandiri','bank_transfer','mandiri','mandiri','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_muamalat','bank_transfer','muamalat','bmi','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_bri','bank_transfer','bri','bri','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_bsi','bank_transfer','bsi','bsi','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_permata','bank_transfer','permata','permata','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_danamon','bank_transfer','danamon','danamon','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','va_btn','bank_transfer','btn','btn','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','retail_alfamart','retail','alfamart','alfamart','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','retail_indomaret','retail','indomaret','indomaret','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','ewallet_dana','wallet','dana','dana','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','ewallet_shopeepay','wallet','shopeepay','shopeepay','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}'),
('ipaymu','paylater_akulaku','paylater','akulaku','akulaku','DOCUMENTED','https://docs.ipaymu.com/en/docs/payment/direct-payment','{}')
ON CONFLICT (provider_code,payment_method_code) DO UPDATE SET
    provider_method=EXCLUDED.provider_method,
    provider_method_type=EXCLUDED.provider_method_type,
    provider_channel_code=EXCLUDED.provider_channel_code,
    source_url=EXCLUDED.source_url,
    metadata=EXCLUDED.metadata,
    updated_at=now();

UPDATE providers p
SET payment_methods = COALESCE((
    SELECT jsonb_agg(c.payment_method_code ORDER BY m.sort_order,c.payment_method_code)
    FROM provider_payment_method_capabilities c
    JOIN payment_methods m ON m.code=c.payment_method_code
    WHERE c.provider_code=p.code AND c.support_status<>'DISABLED'
), '[]'::jsonb),
updated_at=now()
WHERE p.code='ipaymu';
