INSERT INTO provider_versions (id,provider_code,version,engine_kind,status,released_at)
VALUES ('pver_midtrans_emisell_v1','midtrans','emisell-midtrans-v1','isolated_connector','RELEASED',now())
ON CONFLICT (provider_code,version) DO UPDATE SET
    engine_kind='isolated_connector',status='RELEASED',released_at=COALESCE(provider_versions.released_at,now());

UPDATE providers
SET description='Connector Midtrans Core API yang dijalankan terisolasi oleh Emisell Connector Runner.',
    engine_connector='midtrans',
    credential_schema='[
      {"code":"server_key","label":"Server key","input_type":"password","secret":true,"required":true}
    ]'::jsonb,
    environments='["sandbox","live"]'::jsonb,
    available=true,
    updated_at=now()
WHERE code='midtrans';

UPDATE provider_payment_method_capabilities
SET metadata=jsonb_build_object(
        'engine','isolated_connector',
        'engine_support','SUPPORTED',
        'provider_api','midtrans_core_v2',
        'conformance_profile','midtrans-core-v2/' || payment_method_code
    ),
    updated_at=now()
WHERE provider_code='midtrans'
  AND payment_method_code IN (
    'qris','va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb',
    'ewallet_gopay','ewallet_shopeepay'
  );

UPDATE provider_payment_method_capabilities
SET metadata=metadata || '{"engine":"isolated_connector","engine_support":"UNSUPPORTED","blocker_code":"CONNECTOR_METHOD_NOT_IMPLEMENTED"}'::jsonb,
    updated_at=now()
WHERE provider_code='midtrans'
  AND payment_method_code NOT IN (
    'qris','va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb',
    'ewallet_gopay','ewallet_shopeepay'
  );

-- A method stays DOCUMENTED until its own sandbox certification succeeds.
-- Publishing the connector runtime must never turn catalog documentation into
-- production evidence automatically.
