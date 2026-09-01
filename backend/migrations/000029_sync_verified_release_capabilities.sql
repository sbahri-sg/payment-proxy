-- Keep the global capability catalog consistent with published releases that
-- passed automated backend verification. Legacy operator review must never
-- unlock a connector method by itself.
UPDATE provider_payment_method_capabilities AS capability
SET support_status = 'CERTIFIED',
    updated_at = now()
FROM provider_app_versions AS release
WHERE release.provider_code = capability.provider_code
  AND release.status = 'PUBLISHED'
  AND release.verification_report->>'passed' = 'true'
  AND release.verification_report->>'source' = 'automated_backend_verification'
  AND capability.support_status <> 'DISABLED'
  AND EXISTS (
      SELECT 1
      FROM jsonb_array_elements_text(
          COALESCE(release.verification_report->'verified_capabilities', '[]'::jsonb)
      ) AS verified(payment_method_code)
      WHERE verified.payment_method_code = capability.payment_method_code
  );

-- Assignment labels are presentation data derived from the canonical catalog,
-- never merchant input. Normalize pre-existing records as part of the rollout.
UPDATE payment_method_assignments AS assignment
SET label = method.name,
    updated_at = now()
FROM payment_methods AS method
WHERE method.code = assignment.payment_method_code
  AND assignment.label IS DISTINCT FROM method.name;
