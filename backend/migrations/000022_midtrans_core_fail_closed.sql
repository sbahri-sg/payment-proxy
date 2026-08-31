UPDATE providers
SET description='Connector Midtrans Core API yang dijalankan terisolasi oleh Emisell Connector Runner.',
    updated_at=now()
WHERE code='midtrans';

UPDATE provider_payment_method_capabilities
SET metadata=(metadata - 'sandbox_fallback' - 'sandbox_fallback_trigger') ||
        '{"provider_activation_required":true}'::jsonb,
    updated_at=now()
WHERE provider_code='midtrans'
  AND payment_method_code IN ('qris','va_bri','va_cimb','ewallet_gopay','ewallet_shopeepay');
