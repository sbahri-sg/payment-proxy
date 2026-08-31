CREATE TABLE IF NOT EXISTS payment_methods (
    code text PRIMARY KEY CHECK (code ~ '^[a-z0-9_]{2,64}$'),
    category text NOT NULL CHECK (category IN (
        'QR_CODE','CARD','VIRTUAL_ACCOUNT','E_WALLET','RETAIL','PAYLATER','DIRECT_DEBIT','DIGITAL_BANKING'
    )),
    name text NOT NULL CHECK (char_length(name) BETWEEN 2 AND 96),
    description text NOT NULL DEFAULT '',
    countries jsonb NOT NULL DEFAULT '["ID"]'::jsonb,
    currencies jsonb NOT NULL DEFAULT '["IDR"]'::jsonb,
    active boolean NOT NULL DEFAULT true,
    sort_order integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS provider_payment_method_capabilities (
    provider_code text NOT NULL REFERENCES providers(code) ON DELETE CASCADE,
    payment_method_code text NOT NULL REFERENCES payment_methods(code) ON DELETE RESTRICT,
    provider_method text NOT NULL CHECK (provider_method ~ '^[a-z0-9_]{2,64}$'),
    provider_method_type text NOT NULL CHECK (provider_method_type ~ '^[a-z0-9_]{2,64}$'),
    provider_channel_code text NOT NULL DEFAULT '',
    support_status text NOT NULL CHECK (support_status IN ('DOCUMENTED','CERTIFIED','DISABLED')),
    source_url text NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider_code, payment_method_code)
);

CREATE INDEX IF NOT EXISTS provider_payment_method_capabilities_method_idx
    ON provider_payment_method_capabilities (payment_method_code, support_status, provider_code);

INSERT INTO payment_methods (code,category,name,description,sort_order) VALUES
('qris','QR_CODE','QRIS','Pembayaran QR nasional melalui mobile banking atau e-wallet.',10),
('card','CARD','Credit / Debit Card','Kartu Visa, Mastercard, JCB, Amex, atau jaringan yang didukung gateway.',20),
('va_bca','VIRTUAL_ACCOUNT','BCA Virtual Account','Virtual account Bank Central Asia.',101),
('va_mandiri','VIRTUAL_ACCOUNT','Mandiri Virtual Account','Virtual account atau bill payment Bank Mandiri.',102),
('va_bni','VIRTUAL_ACCOUNT','BNI Virtual Account','Virtual account Bank Negara Indonesia.',103),
('va_bri','VIRTUAL_ACCOUNT','BRI Virtual Account','Virtual account Bank Rakyat Indonesia.',104),
('va_permata','VIRTUAL_ACCOUNT','Permata Virtual Account','Virtual account PermataBank.',105),
('va_cimb','VIRTUAL_ACCOUNT','CIMB Niaga Virtual Account','Virtual account CIMB Niaga.',106),
('va_danamon','VIRTUAL_ACCOUNT','Danamon Virtual Account','Virtual account Bank Danamon.',107),
('va_bsi','VIRTUAL_ACCOUNT','BSI Virtual Account','Virtual account Bank Syariah Indonesia.',108),
('va_maybank','VIRTUAL_ACCOUNT','Maybank Virtual Account','Virtual account Maybank Indonesia.',109),
('va_bnc','VIRTUAL_ACCOUNT','BNC Virtual Account','Virtual account Bank Neo Commerce.',110),
('va_btn','VIRTUAL_ACCOUNT','BTN Virtual Account','Virtual account Bank Tabungan Negara.',111),
('va_atm_bersama','VIRTUAL_ACCOUNT','ATM Bersama Virtual Account','Virtual account yang dapat dibayar melalui jaringan ATM Bersama.',112),
('va_arta_graha','VIRTUAL_ACCOUNT','Artha Graha Virtual Account','Virtual account Bank Artha Graha.',113),
('va_sahabat_sampoerna','VIRTUAL_ACCOUNT','Sahabat Sampoerna Virtual Account','Virtual account Bank Sahabat Sampoerna.',114),
('va_muamalat','VIRTUAL_ACCOUNT','Muamalat Virtual Account','Virtual account Bank Muamalat.',115),
('va_doku','VIRTUAL_ACCOUNT','DOKU Virtual Account','Virtual account internal DOKU.',116),
('ewallet_gopay','E_WALLET','GoPay','Pembayaran melalui aplikasi GoPay.',201),
('ewallet_ovo','E_WALLET','OVO','Pembayaran melalui aplikasi OVO.',202),
('ewallet_dana','E_WALLET','DANA','Pembayaran melalui aplikasi DANA.',203),
('ewallet_shopeepay','E_WALLET','ShopeePay','Pembayaran melalui aplikasi ShopeePay.',204),
('ewallet_linkaja','E_WALLET','LinkAja','Pembayaran melalui aplikasi LinkAja.',205),
('ewallet_astrapay','E_WALLET','AstraPay','Pembayaran melalui aplikasi AstraPay.',206),
('ewallet_doku','E_WALLET','DOKU Wallet','Pembayaran melalui DOKU Wallet.',207),
('retail_alfamart','RETAIL','Alfamart / Alfa Group','Pembayaran tunai atau kode pembayaran di jaringan Alfa Group.',301),
('retail_indomaret','RETAIL','Indomaret','Pembayaran tunai atau kode pembayaran di Indomaret.',302),
('retail_pegadaian_pos','RETAIL','Pegadaian / Pos Indonesia','Pembayaran melalui gerai Pegadaian atau Pos Indonesia.',303),
('paylater_kredivo','PAYLATER','Kredivo','Pembayaran paylater atau cicilan Kredivo.',401),
('paylater_akulaku','PAYLATER','Akulaku','Pembayaran paylater atau cicilan Akulaku.',402),
('paylater_indodana','PAYLATER','Indodana','Pembayaran paylater atau cicilan Indodana.',403),
('paylater_atome','PAYLATER','Atome','Pembayaran paylater atau cicilan Atome.',404),
('direct_debit_bri','DIRECT_DEBIT','BRI Direct Debit','Pembayaran direct debit rekening BRI.',501),
('digital_banking_jenius','DIGITAL_BANKING','Jenius Pay','Pembayaran melalui otorisasi Jenius Pay.',601),
('kki','CARD','Kartu Kredit Indonesia','Pembayaran Kartu Kredit Indonesia dengan autentikasi issuer.',602)
ON CONFLICT (code) DO UPDATE SET
    category=EXCLUDED.category,name=EXCLUDED.name,description=EXCLUDED.description,
    sort_order=EXCLUDED.sort_order,updated_at=now();

INSERT INTO providers (
    code,name,description,available,engine_connector,credential_schema,environments,payment_methods
) VALUES (
    'duitku','Duitku','Connector Duitku direncanakan setelah Connector SDK dan conformance test tersedia.',false,'duitku',
    '[{"code":"merchant_code","label":"Merchant code","input_type":"text","secret":false,"required":true},{"code":"api_key","label":"API key","input_type":"password","secret":true,"required":true}]'::jsonb,
    '["sandbox","live"]'::jsonb,'[]'::jsonb
)
ON CONFLICT (code) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,
    credential_schema=EXCLUDED.credential_schema,environments=EXCLUDED.environments,updated_at=now();

INSERT INTO provider_payment_method_capabilities
    (provider_code,payment_method_code,provider_method,provider_method_type,provider_channel_code,support_status,source_url,metadata)
VALUES
-- Xendit: only QRIS is connector-certified in the current Emisell release.
('xendit','qris','real_time_payment','qris','QRIS','CERTIFIED','https://docs.xendit.co/docs/qris','{}'),
('xendit','card','card','card','CARDS','DOCUMENTED','https://docs.xendit.co/docs/cards-api-overview/index.html','{}'),
('xendit','va_bca','bank_transfer','bca','BCA_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_mandiri','bank_transfer','mandiri','MANDIRI_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_bni','bank_transfer','bni','BNI_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_bri','bank_transfer','bri','BRI_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_permata','bank_transfer','permata','PERMATA_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_cimb','bank_transfer','cimb','CIMB_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_danamon','bank_transfer','danamon','DANAMON_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_bsi','bank_transfer','bsi','BSI_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','va_muamalat','bank_transfer','muamalat','MUAMALAT_VIRTUAL_ACCOUNT','DOCUMENTED','https://docs.xendit.co/docs/en/available-payment-channels','{}'),
('xendit','ewallet_ovo','wallet','ovo','OVO','DOCUMENTED','https://docs.xendit.co/id/ewallet','{}'),
('xendit','ewallet_dana','wallet','dana','DANA','DOCUMENTED','https://docs.xendit.co/id/ewallet','{}'),
('xendit','ewallet_shopeepay','wallet','shopeepay','SHOPEEPAY','DOCUMENTED','https://docs.xendit.co/id/ewallet','{}'),
('xendit','ewallet_linkaja','wallet','linkaja','LINKAJA','DOCUMENTED','https://docs.xendit.co/id/ewallet','{}'),
('xendit','ewallet_astrapay','wallet','astrapay','ASTRAPAY','DOCUMENTED','https://docs.xendit.co/id/ewallet','{}'),
('xendit','digital_banking_jenius','digital_banking','jenius','JENIUS_PAY','DOCUMENTED','https://docs.xendit.co/id/ewallet','{}'),
('xendit','paylater_kredivo','paylater','kredivo','KREDIVO','DOCUMENTED','https://docs.xendit.co/paylater/','{}'),
('xendit','paylater_akulaku','paylater','akulaku','AKULAKU','DOCUMENTED','https://docs.xendit.co/paylater/','{}'),
('xendit','paylater_indodana','paylater','indodana','INDODANA','DOCUMENTED','https://docs.xendit.co/paylater/','{}'),

-- Midtrans documented payment channels.
('midtrans','qris','real_time_payment','qris','other_qris','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','card','card','card','credit_card','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_bca','bank_transfer','bca','bca_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_mandiri','bank_transfer','mandiri','echannel','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_bni','bank_transfer','bni','bni_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_bri','bank_transfer','bri','bri_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_permata','bank_transfer','permata','permata_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_cimb','bank_transfer','cimb','cimb_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_danamon','bank_transfer','danamon','danamon_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','va_bsi','bank_transfer','bsi','bsi_va','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','ewallet_gopay','wallet','gopay','gopay','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','ewallet_ovo','wallet','ovo','ovo','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','ewallet_dana','wallet','dana','dana','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','ewallet_shopeepay','wallet','shopeepay','shopeepay','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','retail_alfamart','retail','alfamart','alfamart','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','retail_indomaret','retail','indomaret','indomaret','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','paylater_akulaku','paylater','akulaku','akulaku','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),
('midtrans','paylater_kredivo','paylater','kredivo','kredivo','DOCUMENTED','https://docs.midtrans.com/reference/request-body-json-parameter','{}'),

-- Duitku POP payment codes.
('duitku','qris','real_time_payment','qris','SP','DOCUMENTED','https://docs.duitku.com/pop/id/','{"alternative_channel_codes":["NQ","GQ","SQ"]}'),
('duitku','card','card','card','VC','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_bca','bank_transfer','bca','BC','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_mandiri','bank_transfer','mandiri','M2','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_bni','bank_transfer','bni','I1','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_bri','bank_transfer','bri','BR','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_permata','bank_transfer','permata','BT','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_cimb','bank_transfer','cimb','B1','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_danamon','bank_transfer','danamon','DM','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_bsi','bank_transfer','bsi','BV','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_maybank','bank_transfer','maybank','VA','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_bnc','bank_transfer','bnc','NC','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_atm_bersama','bank_transfer','atm_bersama','A1','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_arta_graha','bank_transfer','arta_graha','AG','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','va_sahabat_sampoerna','bank_transfer','sahabat_sampoerna','S1','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','ewallet_ovo','wallet','ovo','OV','DOCUMENTED','https://docs.duitku.com/pop/id/','{"alternative_channel_codes":["OL"]}'),
('duitku','ewallet_dana','wallet','dana','DA','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','ewallet_shopeepay','wallet','shopeepay','SA','DOCUMENTED','https://docs.duitku.com/pop/id/','{"alternative_channel_codes":["SL"]}'),
('duitku','ewallet_linkaja','wallet','linkaja','LF','DOCUMENTED','https://docs.duitku.com/pop/id/','{"alternative_channel_codes":["LA"]}'),
('duitku','retail_alfamart','retail','alfamart','FT','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','retail_indomaret','retail','indomaret','IR','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','retail_pegadaian_pos','retail','pegadaian_pos','FT','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','paylater_indodana','paylater','indodana','DN','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','paylater_atome','paylater','atome','AT','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),
('duitku','digital_banking_jenius','digital_banking','jenius','JP','DOCUMENTED','https://docs.duitku.com/pop/id/','{}'),

-- DOKU Checkout payment method values.
('doku','qris','real_time_payment','qris','QRIS','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','card','card','card','CREDIT_CARD','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_bca','bank_transfer','bca','VIRTUAL_ACCOUNT_BCA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_mandiri','bank_transfer','mandiri','VIRTUAL_ACCOUNT_BANK_MANDIRI','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_bni','bank_transfer','bni','VIRTUAL_ACCOUNT_BNI','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_bri','bank_transfer','bri','VIRTUAL_ACCOUNT_BRI','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_permata','bank_transfer','permata','VIRTUAL_ACCOUNT_BANK_PERMATA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_cimb','bank_transfer','cimb','VIRTUAL_ACCOUNT_BANK_CIMB','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_danamon','bank_transfer','danamon','VIRTUAL_ACCOUNT_BANK_DANAMON','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_bsi','bank_transfer','bsi','VIRTUAL_ACCOUNT_BANK_SYARIAH_MANDIRI','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_bnc','bank_transfer','bnc','VIRTUAL_ACCOUNT_BNC','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_btn','bank_transfer','btn','VIRTUAL_ACCOUNT_BTN','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','va_doku','bank_transfer','doku','VIRTUAL_ACCOUNT_DOKU','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','ewallet_ovo','wallet','ovo','EMONEY_OVO','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','ewallet_dana','wallet','dana','EMONEY_DANA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','ewallet_shopeepay','wallet','shopeepay','EMONEY_SHOPEE_PAY','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','ewallet_linkaja','wallet','linkaja','EMONEY_LINKAJA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','ewallet_doku','wallet','doku','EMONEY_DOKU','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','retail_alfamart','retail','alfamart','ONLINE_TO_OFFLINE_ALFA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','retail_indomaret','retail','indomaret','ONLINE_TO_OFFLINE_INDOMARET','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','paylater_akulaku','paylater','akulaku','PEER_TO_PEER_AKULAKU','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','paylater_kredivo','paylater','kredivo','PEER_TO_PEER_KREDIVO','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','paylater_indodana','paylater','indodana','PEER_TO_PEER_INDODANA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','direct_debit_bri','direct_debit','bri','DIRECT_DEBIT_BRI','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','digital_banking_jenius','digital_banking','jenius','JENIUS_PAY','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}'),
('doku','kki','card','kki','KARTU_KREDIT_INDONESIA','DOCUMENTED','https://developers.doku.com/accept-payments/doku-checkout/supported-payment-methods','{}')
ON CONFLICT (provider_code,payment_method_code) DO UPDATE SET
    provider_method=EXCLUDED.provider_method,
    provider_method_type=EXCLUDED.provider_method_type,
    provider_channel_code=EXCLUDED.provider_channel_code,
    support_status=EXCLUDED.support_status,
    source_url=EXCLUDED.source_url,
    metadata=EXCLUDED.metadata,
    updated_at=now();

UPDATE providers p
SET payment_methods=(
    SELECT COALESCE(jsonb_agg(c.payment_method_code ORDER BY m.sort_order,c.payment_method_code),'[]'::jsonb)
    FROM provider_payment_method_capabilities c
    JOIN payment_methods m ON m.code=c.payment_method_code
    WHERE c.provider_code=p.code AND c.support_status <> 'DISABLED'
), updated_at=now()
WHERE p.code IN ('xendit','midtrans','duitku','doku');

ALTER TABLE payment_method_assignments
    ADD COLUMN IF NOT EXISTS payment_method_code text REFERENCES payment_methods(code) ON DELETE RESTRICT;

INSERT INTO payment_methods(code,category,name,description,sort_order)
SELECT DISTINCT 'legacy_' || substr(md5(a.payment_method || ':' || a.payment_method_type),1,12),
    'DIGITAL_BANKING',upper(replace(a.payment_method_type,'_',' ')),
    'Legacy assignment retained for backward compatibility.',9999
FROM payment_method_assignments a
WHERE NOT (a.payment_method='real_time_payment' AND a.payment_method_type='qris')
  AND a.payment_method <> 'card'
ON CONFLICT (code) DO NOTHING;

UPDATE payment_method_assignments
SET payment_method_code=CASE
    WHEN payment_method='real_time_payment' AND payment_method_type='qris' THEN 'qris'
    WHEN payment_method='card' THEN 'card'
    WHEN payment_method='bank_transfer' AND payment_method_type='virtual_account' THEN 'legacy_' || substr(md5(payment_method || ':' || payment_method_type),1,12)
    WHEN payment_method='wallet' AND payment_method_type='ewallet' THEN 'legacy_' || substr(md5(payment_method || ':' || payment_method_type),1,12)
    ELSE 'legacy_' || substr(md5(payment_method || ':' || payment_method_type),1,12)
END
WHERE payment_method_code IS NULL;

INSERT INTO payment_methods(code,category,name,description,sort_order)
SELECT DISTINCT a.payment_method_code,'DIGITAL_BANKING',upper(replace(a.payment_method_type,'_',' ')),
    'Legacy assignment retained for backward compatibility.',9999
FROM payment_method_assignments a
LEFT JOIN payment_methods m ON m.code=a.payment_method_code
WHERE m.code IS NULL
ON CONFLICT (code) DO NOTHING;

ALTER TABLE payment_method_assignments
    ALTER COLUMN payment_method_code SET NOT NULL;

ALTER TABLE payment_method_assignments
    DROP CONSTRAINT IF EXISTS payment_method_assignments_tenant_id_environment_payment_me_key;

ALTER TABLE payment_method_assignments
    ADD CONSTRAINT payment_method_assignments_tenant_environment_method_code_key
    UNIQUE (tenant_id,environment,payment_method_code);
