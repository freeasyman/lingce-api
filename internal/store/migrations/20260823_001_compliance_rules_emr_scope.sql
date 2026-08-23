ALTER TABLE compliance_rules
  DROP CONSTRAINT IF EXISTS compliance_rules_scope_check;

ALTER TABLE compliance_rules
  ADD CONSTRAINT compliance_rules_scope_check
  CHECK (scope IN ('communication', 'content', 'emr'));

UPDATE compliance_rules
SET scope = 'emr',
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND (
    id LIKE 'emr-rule-%'
    OR code LIKE 'emr.%'
    OR category IN ('emr_template', 'emr_quality')
  );
