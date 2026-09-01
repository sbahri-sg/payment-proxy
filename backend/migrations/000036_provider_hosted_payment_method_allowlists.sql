-- Hosted checkout must never expose a payment method that the merchant marked
-- INACTIVE. The Kernel now sends every eligible ACTIVE assignment to the
-- provider runtime. Midtrans and DOKU accept an exact list, while Duitku POP
-- can safely pin one method. iPaymu redirect checkout is kept fail-closed
-- because its public per-transaction contract has no exact allowlist.

INSERT INTO provider_versions (id,provider_code,version,engine_kind,status,released_at)
VALUES
    ('pver_midtrans_emisell_v2_0_2','midtrans','emisell-midtrans-v2.0.2','isolated_container','RELEASED',now()),
    ('pver_duitku_emisell_v2_0_2','duitku','emisell-duitku-v2.0.2','isolated_container','RELEASED',now()),
    ('pver_doku_emisell_v2_0_2','doku','emisell-doku-v2.0.2','isolated_container','RELEASED',now()),
    ('pver_ipaymu_emisell_v2_0_2','ipaymu','emisell-ipaymu-v2.0.2','isolated_container','RELEASED',now())
ON CONFLICT(provider_code,version) DO UPDATE SET
    engine_kind=EXCLUDED.engine_kind,
    status='RELEASED',
    released_at=COALESCE(provider_versions.released_at,now());

UPDATE provider_versions
SET status='SUPERSEDED'
WHERE provider_code IN ('midtrans','duitku','doku','ipaymu')
  AND version NOT IN (
      'emisell-midtrans-v2.0.2',
      'emisell-duitku-v2.0.2',
      'emisell-doku-v2.0.2',
      'emisell-ipaymu-v2.0.2'
  )
  AND status='RELEASED';

UPDATE provider_installations
SET provider_version=CASE provider_code
        WHEN 'midtrans' THEN 'emisell-midtrans-v2.0.2'
        WHEN 'duitku' THEN 'emisell-duitku-v2.0.2'
        WHEN 'doku' THEN 'emisell-doku-v2.0.2'
        WHEN 'ipaymu' THEN 'emisell-ipaymu-v2.0.2'
    END,
    updated_by='migration:hosted-payment-method-allowlists',
    updated_at=now(),
    version=version+1
WHERE provider_code IN ('midtrans','duitku','doku','ipaymu')
  AND status<>'UNINSTALLED'
  AND provider_version<>CASE provider_code
        WHEN 'midtrans' THEN 'emisell-midtrans-v2.0.2'
        WHEN 'duitku' THEN 'emisell-duitku-v2.0.2'
        WHEN 'doku' THEN 'emisell-doku-v2.0.2'
        WHEN 'ipaymu' THEN 'emisell-ipaymu-v2.0.2'
    END;

UPDATE providers
SET description='Connector iPaymu API v2 untuk direct payment per channel, status transaksi, dan callback HMAC terverifikasi. Hosted redirect menunggu dukungan allowlist exact per transaksi.',
    updated_at=now()
WHERE code='ipaymu';
