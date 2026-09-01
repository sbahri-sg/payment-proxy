ALTER TABLE provider_app_providers
    ADD COLUMN logo bytea NOT NULL DEFAULT ''::bytea,
    ADD COLUMN logo_content_type text NOT NULL DEFAULT '';

ALTER TABLE provider_app_providers
    ADD CONSTRAINT provider_app_providers_logo_size
        CHECK (octet_length(logo) <= 524288),
    ADD CONSTRAINT provider_app_providers_logo_content_type
        CHECK (
            (octet_length(logo) = 0 AND logo_content_type = '') OR
            (octet_length(logo) > 0 AND logo_content_type IN ('image/png','image/jpeg'))
        );
