CREATE TABLE provider_app_providers (
    provider_code text PRIMARY KEY REFERENCES providers(code) ON DELETE RESTRICT,
    website_url text NOT NULL DEFAULT '',
    documentation_url text NOT NULL DEFAULT '',
    support_email text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','ACTIVE','DISABLED')),
    created_by text NOT NULL,
    updated_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT provider_app_providers_website_length CHECK (length(website_url) <= 500),
    CONSTRAINT provider_app_providers_documentation_length CHECK (length(documentation_url) <= 500),
    CONSTRAINT provider_app_providers_support_email_length CHECK (length(support_email) <= 254)
);

INSERT INTO provider_app_providers(provider_code,status,created_by,updated_by)
SELECT v.provider_code,
       CASE WHEN bool_or(v.status='PUBLISHED') THEN 'ACTIVE' ELSE 'DRAFT' END,
       min(v.submitted_by),
       min(v.submitted_by)
FROM provider_app_versions v
GROUP BY v.provider_code
ON CONFLICT(provider_code) DO NOTHING;

ALTER TABLE provider_app_versions
    ADD CONSTRAINT provider_app_versions_provider_fkey
    FOREIGN KEY(provider_code) REFERENCES providers(code) ON DELETE RESTRICT;

CREATE INDEX provider_app_providers_status_updated_idx
    ON provider_app_providers(status,updated_at DESC);
