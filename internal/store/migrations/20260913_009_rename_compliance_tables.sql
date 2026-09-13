-- Rename compliance tables to formal business names.
-- 本迁移只重命名已存在的合规表、约束和索引，不修改业务数据。
-- 署名：Codex，合规卫士开发 Agent
-- 时间：2026-09-14

BEGIN;

DO $rename_tables$
BEGIN
  IF to_regclass('compliance_mvp_rules') IS NOT NULL
     AND to_regclass('compliance_rules') IS NULL THEN
    ALTER TABLE compliance_mvp_rules RENAME TO compliance_rules;
  END IF;

  IF to_regclass('compliance_mvp_check_results') IS NOT NULL
     AND to_regclass('compliance_check_results') IS NULL THEN
    ALTER TABLE compliance_mvp_check_results RENAME TO compliance_check_results;
  END IF;
END
$rename_tables$;

ALTER TABLE IF EXISTS compliance_rules
  RENAME CONSTRAINT compliance_mvp_rules_status_check TO compliance_rules_status_check;
ALTER TABLE IF EXISTS compliance_rules
  RENAME CONSTRAINT compliance_mvp_rules_subject_check TO compliance_rules_subject_check;
ALTER TABLE IF EXISTS compliance_rules
  RENAME CONSTRAINT compliance_mvp_rules_scene_codes_array_check TO compliance_rules_scene_codes_array_check;
ALTER TABLE IF EXISTS compliance_rules
  RENAME CONSTRAINT compliance_mvp_rules_role_codes_array_check TO compliance_rules_role_codes_array_check;
ALTER TABLE IF EXISTS compliance_rules
  RENAME CONSTRAINT compliance_mvp_rules_definition_object_check TO compliance_rules_definition_object_check;
ALTER TABLE IF EXISTS compliance_rules
  RENAME CONSTRAINT compliance_mvp_rules_identity_uq TO compliance_rules_identity_uq;

ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_tenant_check TO compliance_check_results_tenant_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_subject_check TO compliance_check_results_subject_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_status_check TO compliance_check_results_status_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_finding_count_check TO compliance_check_results_finding_count_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_finding_rule_codes_array_check TO compliance_check_results_finding_rule_codes_array_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_raw_response_object_check TO compliance_check_results_raw_response_object_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_result_json_object_check TO compliance_check_results_result_json_object_check;
ALTER TABLE IF EXISTS compliance_check_results
  RENAME CONSTRAINT compliance_mvp_check_results_execution_key_uq TO compliance_check_results_execution_key_uq;

ALTER INDEX IF EXISTS compliance_mvp_rules_lookup_idx RENAME TO compliance_rules_lookup_idx;
ALTER INDEX IF EXISTS compliance_mvp_rules_scene_codes_gin_idx RENAME TO compliance_rules_scene_codes_gin_idx;
ALTER INDEX IF EXISTS compliance_mvp_rules_role_codes_gin_idx RENAME TO compliance_rules_role_codes_gin_idx;

ALTER INDEX IF EXISTS compliance_mvp_check_results_tenant_time_idx RENAME TO compliance_check_results_tenant_time_idx;
ALTER INDEX IF EXISTS compliance_mvp_check_results_source_idx RENAME TO compliance_check_results_source_idx;
ALTER INDEX IF EXISTS compliance_mvp_check_results_status_idx RENAME TO compliance_check_results_status_idx;
ALTER INDEX IF EXISTS compliance_mvp_check_results_rule_codes_gin_idx RENAME TO compliance_check_results_rule_codes_gin_idx;

COMMIT;
