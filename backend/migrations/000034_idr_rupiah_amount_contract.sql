-- IDR amounts are canonical whole rupiah values from v2 onward.
-- The previous runtimes interpreted the API value as a two-decimal minor unit
-- and divided it by 100 before calling providers. All supported Indonesian
-- providers already accept whole rupiah, so active installations are moved as
-- one coordinated release to prevent a merchant from receiving the old amount
-- behavior after deployment.

INSERT INTO provider_versions (id,provider_code,version,engine_kind,status,released_at)
VALUES
    ('pver_xendit_emisell_v2_0_1','xendit','emisell-xendit-v2.0.1','isolated_container','RELEASED',now()),
    ('pver_midtrans_emisell_v2_0_1','midtrans','emisell-midtrans-v2.0.1','isolated_container','RELEASED',now()),
    ('pver_duitku_emisell_v2_0_1','duitku','emisell-duitku-v2.0.1','isolated_container','RELEASED',now()),
    ('pver_doku_emisell_v2_0_1','doku','emisell-doku-v2.0.1','isolated_container','RELEASED',now()),
    ('pver_ipaymu_emisell_v2_0_1','ipaymu','emisell-ipaymu-v2.0.1','isolated_container','RELEASED',now())
ON CONFLICT(provider_code,version) DO UPDATE SET
    engine_kind=EXCLUDED.engine_kind,
    status='RELEASED',
    released_at=COALESCE(provider_versions.released_at,now());

UPDATE provider_versions
SET status='SUPERSEDED'
WHERE provider_code IN ('xendit','midtrans','duitku','doku','ipaymu')
  AND version NOT IN (
      'emisell-xendit-v2.0.1',
      'emisell-midtrans-v2.0.1',
      'emisell-duitku-v2.0.1',
      'emisell-doku-v2.0.1',
      'emisell-ipaymu-v2.0.1'
  )
  AND status='RELEASED';

UPDATE provider_installations
SET provider_version=CASE provider_code
        WHEN 'xendit' THEN 'emisell-xendit-v2.0.1'
        WHEN 'midtrans' THEN 'emisell-midtrans-v2.0.1'
        WHEN 'duitku' THEN 'emisell-duitku-v2.0.1'
        WHEN 'doku' THEN 'emisell-doku-v2.0.1'
        WHEN 'ipaymu' THEN 'emisell-ipaymu-v2.0.1'
    END,
    updated_by='migration:idr-rupiah-contract',
    updated_at=now(),
    version=version+1
WHERE provider_code IN ('xendit','midtrans','duitku','doku','ipaymu')
  AND status<>'UNINSTALLED'
  AND provider_version<>CASE provider_code
        WHEN 'xendit' THEN 'emisell-xendit-v2.0.1'
        WHEN 'midtrans' THEN 'emisell-midtrans-v2.0.1'
        WHEN 'duitku' THEN 'emisell-duitku-v2.0.1'
        WHEN 'doku' THEN 'emisell-doku-v2.0.1'
        WHEN 'ipaymu' THEN 'emisell-ipaymu-v2.0.1'
    END;
