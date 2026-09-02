-- Midtrans Snap supports a broader hosted-checkout catalog than the Core API
-- methods implemented by the connector. Publish those channels explicitly so
-- merchants can select them, while retaining direct_support metadata for the
-- smaller Core API subset.

INSERT INTO provider_versions (id,provider_code,version,engine_kind,status,released_at)
VALUES ('pver_midtrans_emisell_v2_0_3','midtrans','emisell-midtrans-v2.0.3','isolated_container','RELEASED',now())
ON CONFLICT(provider_code,version) DO UPDATE SET
    engine_kind=EXCLUDED.engine_kind,
    status='RELEASED',
    released_at=COALESCE(provider_versions.released_at,now());

UPDATE provider_versions
SET status='SUPERSEDED'
WHERE provider_code='midtrans'
  AND version<>'emisell-midtrans-v2.0.3'
  AND status='RELEASED';

UPDATE provider_installations
SET provider_version='emisell-midtrans-v2.0.3',
    updated_by='migration:midtrans-snap-catalog',
    updated_at=now(),
    version=version+1
WHERE provider_code='midtrans'
  AND status<>'UNINSTALLED'
  AND provider_version<>'emisell-midtrans-v2.0.3';

UPDATE providers
SET description='Connector Midtrans Core API dan Snap hosted checkout dengan allowlist metode per transaksi.',
    payment_methods='[
      "qris","card",
      "va_bca","va_mandiri","va_bni","va_bri","va_permata","va_cimb","va_danamon","va_bsi",
      "ewallet_gopay","ewallet_ovo","ewallet_dana","ewallet_shopeepay",
      "retail_alfamart","retail_indomaret",
      "paylater_akulaku","paylater_kredivo"
    ]'::jsonb,
    updated_at=now()
WHERE code='midtrans';

UPDATE provider_payment_method_capabilities
SET metadata=(metadata - 'blocker_code' - 'conformance_profile' - 'provider_api') ||
        jsonb_build_object(
            'engine','isolated_container',
            'engine_support','SUPPORTED',
            'hosted_support','SUPPORTED',
            'direct_support',CASE
                WHEN payment_method_code IN (
                    'qris','va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb',
                    'ewallet_gopay','ewallet_shopeepay'
                ) THEN 'SUPPORTED'
                ELSE 'UNSUPPORTED'
            END,
            'provider_api',CASE
                WHEN payment_method_code IN (
                    'qris','va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb',
                    'ewallet_gopay','ewallet_shopeepay'
                ) THEN 'midtrans_core_v2'
                ELSE 'midtrans_snap_v1'
            END,
            'conformance_profile',CASE
                WHEN payment_method_code IN (
                    'qris','va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb',
                    'ewallet_gopay','ewallet_shopeepay'
                ) THEN 'midtrans-core-v2/' || payment_method_code
                ELSE 'midtrans-snap-v1/' || payment_method_code
            END
        ),
    updated_at=now()
WHERE provider_code='midtrans'
  AND payment_method_code IN (
    'qris','card',
    'va_bca','va_mandiri','va_bni','va_bri','va_permata','va_cimb','va_danamon','va_bsi',
    'ewallet_gopay','ewallet_ovo','ewallet_dana','ewallet_shopeepay',
    'retail_alfamart','retail_indomaret',
    'paylater_akulaku','paylater_kredivo'
  );
