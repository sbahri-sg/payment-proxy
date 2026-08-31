UPDATE providers
SET description='Connector Midtrans Core API dengan fallback Snap khusus sandbox yang dijalankan terisolasi oleh Emisell Connector Runner.',
    credential_schema='[
      {"code":"server_key","label":"Server key","input_type":"password","secret":true,"required":true},
      {"code":"pop_id","label":"PoP ID (Core API)","input_type":"password","secret":true,"required":false}
    ]'::jsonb,
    updated_at=now()
WHERE code='midtrans';

UPDATE provider_payment_method_capabilities
SET metadata=metadata || '{"sandbox_fallback":"midtrans_snap","sandbox_fallback_trigger":"core_channel_unavailable"}'::jsonb,
    updated_at=now()
WHERE provider_code='midtrans'
  AND payment_method_code IN (
    'qris','va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb',
    'ewallet_gopay','ewallet_shopeepay'
  );
