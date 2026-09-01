ALTER TABLE provider_app_versions
    ADD COLUMN IF NOT EXISTS verification_report jsonb NOT NULL DEFAULT '{}'::jsonb;

DO $$
BEGIN
    ALTER TABLE provider_app_versions
        ADD CONSTRAINT provider_app_versions_verification_report_object
        CHECK (jsonb_typeof(verification_report) = 'object');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

UPDATE provider_app_versions
SET verification_report = jsonb_build_object(
    'passed', true,
    'source', 'legacy_operator_review',
    'checks', jsonb_build_array(),
    'verified_capabilities', manifest->'payment_methods'
)
WHERE status IN ('CERTIFIED', 'PUBLISHED', 'DEPRECATED')
  AND verification_report = '{}'::jsonb;

COMMENT ON COLUMN provider_app_versions.verification_report IS
    'Read-only backend release verification evidence. CERTIFIED remains the internal compatibility status; dashboards present it as VERIFIED.';
