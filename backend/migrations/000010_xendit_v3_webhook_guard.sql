UPDATE providers
SET credential_schema='[
  {"code":"api_key","label":"Secret API key","input_type":"password","secret":true,"required":true},
  {"code":"webhook_verification_token","label":"Payments API v3 webhook token","input_type":"password","secret":true,"required":true}
]'::jsonb,
updated_at=now()
WHERE code='xendit';

UPDATE provider_installations
SET credential_metadata=credential_metadata - 'webhook_configured' || '{"webhook_ready":false}'::jsonb,
updated_at=now(),
version=version+1
WHERE provider_code='xendit' AND status <> 'UNINSTALLED';
