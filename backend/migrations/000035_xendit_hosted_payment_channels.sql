-- Xendit Payment Sessions show every account channel when
-- allowed_payment_channels is omitted. Release a new immutable runtime that
-- receives each installation's ACTIVE assignments from the control plane and
-- forwards only their canonical Xendit channel codes.

INSERT INTO provider_versions (id,provider_code,version,engine_kind,status,released_at)
VALUES ('pver_xendit_emisell_v2_0_2','xendit','emisell-xendit-v2.0.2','isolated_container','RELEASED',now())
ON CONFLICT(provider_code,version) DO UPDATE SET
    engine_kind=EXCLUDED.engine_kind,
    status='RELEASED',
    released_at=COALESCE(provider_versions.released_at,now());

UPDATE provider_versions
SET status='SUPERSEDED'
WHERE provider_code='xendit'
  AND version<>'emisell-xendit-v2.0.2'
  AND status='RELEASED';

UPDATE provider_installations
SET provider_version='emisell-xendit-v2.0.2',
    updated_by='migration:xendit-hosted-payment-channels',
    updated_at=now(),
    version=version+1
WHERE provider_code='xendit'
  AND status<>'UNINSTALLED'
  AND provider_version<>'emisell-xendit-v2.0.2';
