DROP INDEX IF EXISTS provider_installations_merchant_identity_idx;

ALTER TABLE provider_installations
    DROP COLUMN IF EXISTS merchant_name,
    DROP COLUMN IF EXISTS store_id,
    DROP COLUMN IF EXISTS store_name;
