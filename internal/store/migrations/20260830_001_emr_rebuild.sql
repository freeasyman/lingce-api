-- EMR development rebuild. This migration intentionally replaces the previous
-- development-only EMR tables; it is not a production data migration.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

DROP TABLE IF EXISTS emr_process_records CASCADE;
DROP TABLE IF EXISTS emr_rule_doctor_actions CASCADE;
DROP TABLE IF EXISTS emr_rule_evidences CASCADE;
DROP TABLE IF EXISTS emr_rule_summaries CASCADE;
DROP TABLE IF EXISTS emr_rule_hits CASCADE;
DROP TABLE IF EXISTS emr_rule_runs CASCADE;
DROP TABLE IF EXISTS emr_template_rule_bindings CASCADE;
DROP TABLE IF EXISTS emr_field_events CASCADE;
DROP TABLE IF EXISTS emr_document_versions CASCADE;
DROP TABLE IF EXISTS emr_outpatient_records CASCADE;
DROP TABLE IF EXISTS emr_check_results CASCADE;
DROP TABLE IF EXISTS emr_check_runs CASCADE;
DROP TABLE IF EXISTS emr_template_quality_requirements CASCADE;
DROP TABLE IF EXISTS emr_template_sections CASCADE;
DROP TABLE IF EXISTS emr_template_versions CASCADE;
DROP TABLE IF EXISTS emr_templates CASCADE;
DROP TABLE IF EXISTS emr_ai_candidates CASCADE;
DROP TABLE IF EXISTS emr_record_snapshots CASCADE;
DROP TABLE IF EXISTS emr_records CASCADE;
DROP TABLE IF EXISTS emr_quality_requirements CASCADE;

CREATE TABLE emr_quality_requirements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT,
  code VARCHAR(160) NOT NULL,
  name VARCHAR(255) NOT NULL,
  rule_type VARCHAR(32) NOT NULL,
  quality_group VARCHAR(128) NOT NULL DEFAULT '',
  source_name VARCHAR(255) NOT NULL DEFAULT '',
  source_version TEXT NOT NULL DEFAULT '',
  evaluated_fact TEXT NOT NULL,
  pass_condition TEXT NOT NULL,
  precondition TEXT NOT NULL DEFAULT '',
  evidence_basis TEXT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'published',
  created_by BIGINT NOT NULL DEFAULT 0,
  published_at TIMESTAMPTZ,
  published_by BIGINT,
  disabled_at TIMESTAMPTZ,
  disabled_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_quality_requirements_type_check CHECK (rule_type IN ('缺项', '逻辑冲突', '风险提醒', '归档拦截', '专科要求')),
  CONSTRAINT emr_quality_requirements_status_check CHECK (status IN ('draft', 'published', 'disabled'))
);
CREATE UNIQUE INDEX emr_quality_requirements_scope_code_uq
  ON emr_quality_requirements (COALESCE(tenant_id, 0), code);
CREATE INDEX emr_quality_requirements_scope_status_idx
  ON emr_quality_requirements (tenant_id, status, quality_group, code);

CREATE TABLE emr_templates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT,
  code VARCHAR(128) NOT NULL,
  name VARCHAR(255) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'enabled',
  created_by BIGINT NOT NULL DEFAULT 0,
  updated_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_templates_status_check CHECK (status IN ('enabled', 'disabled'))
);
CREATE UNIQUE INDEX emr_templates_scope_code_uq ON emr_templates (COALESCE(tenant_id, 0), code);

CREATE TABLE emr_template_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_id UUID NOT NULL REFERENCES emr_templates(id),
  version_no VARCHAR(32) NOT NULL,
  name VARCHAR(255) NOT NULL,
  document_type VARCHAR(16) NOT NULL,
  visit_type VARCHAR(16) NOT NULL,
  department_id BIGINT,
  specialty_module VARCHAR(64),
  print_title VARCHAR(255) NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  created_by BIGINT NOT NULL DEFAULT 0,
  published_at TIMESTAMPTZ,
  published_by BIGINT,
  disabled_at TIMESTAMPTZ,
  disabled_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_template_versions_type_check CHECK (document_type IN ('门诊病历', '急诊病历')),
  CONSTRAINT emr_template_versions_visit_check CHECK (visit_type IN ('初诊', '复诊', '通用')),
  CONSTRAINT emr_template_versions_status_check CHECK (status IN ('draft', 'published', 'disabled')),
  CONSTRAINT emr_template_versions_specialty_check CHECK (specialty_module IS NULL OR specialty_module <> '')
);
CREATE UNIQUE INDEX emr_template_versions_template_version_uq ON emr_template_versions (template_id, version_no);

CREATE TABLE emr_template_sections (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_version_id UUID NOT NULL REFERENCES emr_template_versions(id) ON DELETE CASCADE,
  code VARCHAR(128) NOT NULL,
  name VARCHAR(255) NOT NULL,
  content_category VARCHAR(32) NOT NULL,
  input_type VARCHAR(32) NOT NULL,
  is_common BOOLEAN NOT NULL DEFAULT TRUE,
  is_visible BOOLEAN NOT NULL DEFAULT TRUE,
  display_order INTEGER NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  options_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  structure_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT emr_template_sections_category_check CHECK (content_category IN ('就诊归属', '病史', '诊疗依据', '医疗判断', '医疗措施', '文书责任'))
);
CREATE UNIQUE INDEX emr_template_sections_version_code_uq ON emr_template_sections (template_version_id, code);
CREATE UNIQUE INDEX emr_template_sections_version_order_uq ON emr_template_sections (template_version_id, display_order);

CREATE TABLE emr_template_quality_requirements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  template_version_id UUID NOT NULL REFERENCES emr_template_versions(id) ON DELETE CASCADE,
  quality_requirement_id UUID NOT NULL REFERENCES emr_quality_requirements(id),
  execution_mode VARCHAR(16) NOT NULL,
  deadline_action VARCHAR(24) NOT NULL,
  display_order INTEGER NOT NULL DEFAULT 0,
  CONSTRAINT emr_template_quality_execution_check CHECK (execution_mode IN ('程序判断', '大模型判断', '人工判断')),
  CONSTRAINT emr_template_quality_deadline_check CHECK (deadline_action IN ('仅提示', '提交前处理', '确认前处理', '归档前处理', '归档后质控'))
);
CREATE UNIQUE INDEX emr_template_quality_version_requirement_uq
  ON emr_template_quality_requirements (template_version_id, quality_requirement_id);

CREATE TABLE emr_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  encounter_id BIGINT,
  patient_id BIGINT,
  patient_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  template_version_id UUID NOT NULL REFERENCES emr_template_versions(id),
  document_type VARCHAR(16) NOT NULL,
  visit_type VARCHAR(16) NOT NULL,
  department_id BIGINT,
  doctor_id BIGINT NOT NULL,
  specialty_module VARCHAR(64),
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ,
  encounter_context JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_references JSONB NOT NULL DEFAULT '{}'::jsonb,
  working_content JSONB NOT NULL DEFAULT '{}'::jsonb,
  working_digest VARCHAR(128) NOT NULL DEFAULT '',
  current_snapshot_id UUID,
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  revision_no INTEGER NOT NULL DEFAULT 0,
  confirmed_by BIGINT,
  confirmed_at TIMESTAMPTZ,
  confirmed_snapshot_id UUID,
  archived_snapshot_id UUID,
  submitted_at TIMESTAMPTZ,
  archived_at TIMESTAMPTZ,
  voided_at TIMESTAMPTZ,
  created_by BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_saved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_records_document_type_check CHECK (document_type IN ('门诊病历', '急诊病历')),
  CONSTRAINT emr_records_visit_type_check CHECK (visit_type IN ('初诊', '复诊')),
  CONSTRAINT emr_records_status_check CHECK (status IN ('草稿', '需补全', '待确认', '退回', '已确认', '已归档', '已作废'))
);
CREATE INDEX emr_records_tenant_status_idx ON emr_records (tenant_id, status, updated_at DESC);
CREATE INDEX emr_records_tenant_encounter_idx ON emr_records (tenant_id, encounter_id);
CREATE UNIQUE INDEX emr_records_tenant_encounter_uq
  ON emr_records (tenant_id, encounter_id)
  WHERE encounter_id IS NOT NULL;

CREATE TABLE emr_record_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  record_id UUID NOT NULL REFERENCES emr_records(id) ON DELETE CASCADE,
  revision_no INTEGER NOT NULL,
  snapshot_no INTEGER NOT NULL,
  template_version_id UUID NOT NULL REFERENCES emr_template_versions(id),
  patient_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
  document_context JSONB NOT NULL DEFAULT '{}'::jsonb,
  content JSONB NOT NULL DEFAULT '{}'::jsonb,
  content_digest VARCHAR(128) NOT NULL,
  formed_by BIGINT NOT NULL,
  formed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX emr_record_snapshots_record_no_uq ON emr_record_snapshots (record_id, snapshot_no);
CREATE INDEX emr_record_snapshots_record_time_idx ON emr_record_snapshots (record_id, formed_at DESC);

ALTER TABLE emr_records
  ADD CONSTRAINT emr_records_current_snapshot_fk FOREIGN KEY (current_snapshot_id) REFERENCES emr_record_snapshots(id),
  ADD CONSTRAINT emr_records_confirmed_snapshot_fk FOREIGN KEY (confirmed_snapshot_id) REFERENCES emr_record_snapshots(id),
  ADD CONSTRAINT emr_records_archived_snapshot_fk FOREIGN KEY (archived_snapshot_id) REFERENCES emr_record_snapshots(id);

CREATE TABLE emr_ai_candidates (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  record_id UUID NOT NULL REFERENCES emr_records(id) ON DELETE CASCADE,
  section_code VARCHAR(128) NOT NULL,
  content JSONB NOT NULL DEFAULT '{}'::jsonb,
  source_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
  status VARCHAR(16) NOT NULL DEFAULT '待处理',
  generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  handled_at TIMESTAMPTZ,
  handled_by BIGINT,
  CONSTRAINT emr_ai_candidates_status_check CHECK (status IN ('待处理', '已采纳', '已拒绝', '已失效'))
);
CREATE INDEX emr_ai_candidates_record_status_idx ON emr_ai_candidates (record_id, status, generated_at DESC);

CREATE TABLE emr_check_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  record_id UUID NOT NULL REFERENCES emr_records(id) ON DELETE CASCADE,
  snapshot_id UUID NOT NULL REFERENCES emr_record_snapshots(id),
  template_version_id UUID NOT NULL REFERENCES emr_template_versions(id),
  trigger_action VARCHAR(16) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT '执行中',
  overall_result VARCHAR(16) NOT NULL DEFAULT '无法完成',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ,
  started_by BIGINT,
  failure_reason TEXT NOT NULL DEFAULT '',
  CONSTRAINT emr_check_runs_trigger_check CHECK (trigger_action IN ('手动保存', '提交', '确认', '归档', '人工重新检查')),
  CONSTRAINT emr_check_runs_status_check CHECK (status IN ('执行中', '已完成', '执行失败')),
  CONSTRAINT emr_check_runs_result_check CHECK (overall_result IN ('无问题', '存在提示', '需要处理', '无法完成'))
);
CREATE INDEX emr_check_runs_record_time_idx ON emr_check_runs (record_id, started_at DESC);

CREATE TABLE emr_check_results (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  check_run_id UUID NOT NULL REFERENCES emr_check_runs(id) ON DELETE CASCADE,
  quality_requirement_id UUID NOT NULL REFERENCES emr_quality_requirements(id),
  requirement_code VARCHAR(160) NOT NULL,
  requirement_name VARCHAR(255) NOT NULL,
  configured_execution_mode VARCHAR(16) NOT NULL,
  actual_execution_mode VARCHAR(16) NOT NULL,
  conclusion VARCHAR(16) NOT NULL,
  check_status VARCHAR(16) NOT NULL,
  incomplete_reason VARCHAR(16) NOT NULL DEFAULT '',
  handling_result VARCHAR(16) NOT NULL,
  meets_deadline BOOLEAN,
  evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
  hit_explanation TEXT NOT NULL DEFAULT '',
  suggested_handling TEXT NOT NULL DEFAULT '',
  manual_conclusion VARCHAR(16),
  manual_by BIGINT,
  manual_at TIMESTAMPTZ,
  CONSTRAINT emr_check_results_execution_check CHECK (configured_execution_mode IN ('程序判断', '大模型判断', '人工判断') AND actual_execution_mode IN ('程序判断', '大模型判断', '人工判断')),
  CONSTRAINT emr_check_results_conclusion_check CHECK (conclusion IN ('符合', '不符合', '无法判断')),
  CONSTRAINT emr_check_results_status_check CHECK (check_status IN ('已完成', '未完成')),
  CONSTRAINT emr_check_results_handling_check CHECK (handling_result IN ('仅提示', '待医生处理', '待人工判断', '允许继续', '阻断')),
  CONSTRAINT emr_check_results_manual_check CHECK (manual_conclusion IS NULL OR manual_conclusion IN ('符合', '不符合'))
);
CREATE UNIQUE INDEX emr_check_results_run_requirement_uq ON emr_check_results (check_run_id, quality_requirement_id);
CREATE INDEX emr_check_results_run_handling_idx ON emr_check_results (check_run_id, handling_result, conclusion);

CREATE TABLE emr_process_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  record_id UUID REFERENCES emr_records(id) ON DELETE SET NULL,
  action_type VARCHAR(24) NOT NULL,
  action_result VARCHAR(8) NOT NULL,
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  actor_type VARCHAR(8) NOT NULL,
  actor_id BIGINT,
  source VARCHAR(16) NOT NULL,
  before_status VARCHAR(16),
  after_status VARCHAR(16),
  action_snapshot_id UUID REFERENCES emr_record_snapshots(id),
  before_snapshot_id UUID REFERENCES emr_record_snapshots(id),
  after_snapshot_id UUID REFERENCES emr_record_snapshots(id),
  check_run_id UUID REFERENCES emr_check_runs(id),
  ai_candidate_id UUID REFERENCES emr_ai_candidates(id),
  output_info JSONB NOT NULL DEFAULT '{}'::jsonb,
  failure_reason TEXT NOT NULL DEFAULT '',
  action_note TEXT NOT NULL DEFAULT '',
  content_changes JSONB NOT NULL DEFAULT '[]'::jsonb,
  CONSTRAINT emr_process_action_result_check CHECK (action_result IN ('成功', '失败')),
  CONSTRAINT emr_process_actor_type_check CHECK (actor_type IN ('人工', '系统'))
);
CREATE INDEX emr_process_records_record_time_idx ON emr_process_records (record_id, occurred_at DESC, id DESC);
CREATE INDEX emr_process_records_tenant_time_idx ON emr_process_records (tenant_id, occurred_at DESC, id DESC);
