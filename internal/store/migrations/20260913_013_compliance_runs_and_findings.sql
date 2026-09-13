-- Separate compliance execution runs from user-facing findings.
-- 保留已有执行数据，只重命名执行表并新增发现表。
-- 署名：Codex，合规卫士开发 Agent
-- 时间：2026-09-14

BEGIN;

DO $rename_run$
BEGIN
  IF to_regclass('compliance_check_results') IS NOT NULL
     AND to_regclass('compliance_check_runs') IS NULL THEN
    ALTER TABLE compliance_check_results RENAME TO compliance_check_runs;
  END IF;
END
$rename_run$;

DO $rename_run_constraints$
BEGIN
  IF to_regclass('compliance_check_runs') IS NOT NULL THEN
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_tenant_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_tenant_check TO compliance_check_runs_tenant_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_subject_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_subject_check TO compliance_check_runs_subject_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_status_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_status_check TO compliance_check_runs_status_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_finding_count_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_finding_count_check TO compliance_check_runs_finding_count_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_finding_rule_codes_array_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_finding_rule_codes_array_check TO compliance_check_runs_finding_rule_codes_array_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_raw_response_object_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_raw_response_object_check TO compliance_check_runs_raw_response_object_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_result_json_object_check'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_result_json_object_check TO compliance_check_runs_result_json_object_check;
    END IF;
    IF EXISTS (
      SELECT 1 FROM pg_constraint
      WHERE conrelid = 'compliance_check_runs'::regclass
        AND conname = 'compliance_check_results_execution_key_uq'
    ) THEN
      ALTER TABLE compliance_check_runs
        RENAME CONSTRAINT compliance_check_results_execution_key_uq TO compliance_check_runs_execution_key_uq;
    END IF;
  END IF;
END
$rename_run_constraints$;

ALTER INDEX IF EXISTS compliance_check_results_tenant_time_idx RENAME TO compliance_check_runs_tenant_time_idx;
ALTER INDEX IF EXISTS compliance_check_results_source_idx RENAME TO compliance_check_runs_source_idx;
ALTER INDEX IF EXISTS compliance_check_results_status_idx RENAME TO compliance_check_runs_status_idx;
ALTER INDEX IF EXISTS compliance_check_results_rule_codes_gin_idx RENAME TO compliance_check_runs_rule_codes_gin_idx;

CREATE TABLE IF NOT EXISTS compliance_findings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES compliance_check_runs(id) ON DELETE CASCADE,
  tenant_id BIGINT NOT NULL,
  source_type VARCHAR(48) NOT NULL,
  source_id VARCHAR(128) NOT NULL,
  source_version VARCHAR(128) NOT NULL,
  subject VARCHAR(32) NOT NULL,
  scene_code VARCHAR(128) NOT NULL,
  role_code VARCHAR(64) NOT NULL DEFAULT '',
  employee_id BIGINT,
  rule_code VARCHAR(128) NOT NULL,
  rule_version VARCHAR(64) NOT NULL DEFAULT '',
  fact TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  reason TEXT NOT NULL DEFAULT '',
  evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
  missing_facts JSONB NOT NULL DEFAULT '[]'::jsonb,
  needs_review BOOLEAN NOT NULL DEFAULT TRUE,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  review_conclusion VARCHAR(32) NOT NULL DEFAULT '',
  review_reason TEXT NOT NULL DEFAULT '',
  reviewed_by BIGINT,
  reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT compliance_findings_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT compliance_findings_subject_check CHECK (subject IN ('communication', 'content')),
  CONSTRAINT compliance_findings_status_check CHECK (
    status IN ('pending', 'confirmed', 'not_confirmed', 'unable_to_determine')
  ),
  CONSTRAINT compliance_findings_evidence_array_check CHECK (jsonb_typeof(evidence) = 'array'),
  CONSTRAINT compliance_findings_missing_facts_array_check CHECK (jsonb_typeof(missing_facts) = 'array'),
  CONSTRAINT compliance_findings_run_rule_uq UNIQUE (run_id, rule_code)
);

CREATE INDEX IF NOT EXISTS compliance_findings_tenant_status_time_idx
  ON compliance_findings (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_findings_source_idx
  ON compliance_findings (tenant_id, source_type, source_id, source_version);
CREATE INDEX IF NOT EXISTS compliance_findings_rule_code_idx
  ON compliance_findings (tenant_id, rule_code, created_at DESC);
CREATE INDEX IF NOT EXISTS compliance_findings_run_idx
  ON compliance_findings (run_id);

COMMIT;
