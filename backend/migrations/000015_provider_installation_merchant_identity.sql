ALTER TABLE provider_installations
    ADD COLUMN merchant_name text NOT NULL DEFAULT '',
    ADD COLUMN store_id text NOT NULL DEFAULT '',
    ADD COLUMN store_name text NOT NULL DEFAULT '';

UPDATE provider_installations
SET merchant_name = tenant_id
WHERE merchant_name = '';

ALTER TABLE provider_installations
    ADD CONSTRAINT provider_installations_merchant_name_length
        CHECK (char_length(merchant_name) BETWEEN 1 AND 160),
    ADD CONSTRAINT provider_installations_store_id_format
        CHECK (store_id = '' OR store_id ~ '^[A-Za-z0-9._:-]{1,128}$'),
    ADD CONSTRAINT provider_installations_store_name_length
        CHECK (char_length(store_name) <= 160);

CREATE INDEX provider_installations_merchant_identity_idx
    ON provider_installations (tenant_id, store_id, provider_code, environment, updated_at DESC);
