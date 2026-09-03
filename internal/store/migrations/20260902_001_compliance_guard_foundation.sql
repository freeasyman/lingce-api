-- Compliance Guard foundation. This domain is deliberately independent from
-- the legacy compliance_* tables and does not take ownership of source data.

CREATE TABLE IF NOT EXISTS guard_source_refs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  subject VARCHAR(32) NOT NULL,
  source_type VARCHAR(48) NOT NULL,
  source_id VARCHAR(128) NOT NULL,
  source_version VARCHAR(128) NOT NULL DEFAULT '',
  source_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
  employee_id BIGINT,
  department_id BIGINT,
  encounter_id BIGINT,
  source_status VARCHAR(32) NOT NULL DEFAULT 'available',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_source_refs_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_source_refs_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_source_refs_status_check CHECK (source_status IN ('available', 'waiting', 'unavailable', 'deleted'))
);
CREATE UNIQUE INDEX IF NOT EXISTS guard_source_refs_identity_uq
  ON guard_source_refs (tenant_id, source_type, source_id, source_version);
CREATE INDEX IF NOT EXISTS guard_source_refs_tenant_subject_idx
  ON guard_source_refs (tenant_id, subject, updated_at DESC);

CREATE TABLE IF NOT EXISTS guard_source_snapshots (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  source_ref_id UUID NOT NULL REFERENCES guard_source_refs(id),
  source_version VARCHAR(128) NOT NULL DEFAULT '',
  source_fingerprint VARCHAR(128) NOT NULL,
  snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_source_snapshots_tenant_check CHECK (tenant_id > 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS guard_source_snapshots_ref_fingerprint_uq
  ON guard_source_snapshots (source_ref_id, source_fingerprint);
CREATE INDEX IF NOT EXISTS guard_source_snapshots_tenant_time_idx
  ON guard_source_snapshots (tenant_id, captured_at DESC);

CREATE TABLE IF NOT EXISTS guard_analysis_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  source_ref_id UUID NOT NULL REFERENCES guard_source_refs(id),
  source_snapshot_id UUID REFERENCES guard_source_snapshots(id),
  strategy_code VARCHAR(128) NOT NULL,
  strategy_version VARCHAR(64) NOT NULL DEFAULT '',
  rule_set_version VARCHAR(128) NOT NULL DEFAULT '',
  input_fingerprint VARCHAR(128) NOT NULL DEFAULT '',
  status VARCHAR(24) NOT NULL DEFAULT 'queued',
  worker_version VARCHAR(128) NOT NULL DEFAULT '',
  model_version VARCHAR(128) NOT NULL DEFAULT '',
  machine_finding_count INTEGER NOT NULL DEFAULT 0,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  failure_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_analysis_runs_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_analysis_runs_status_check CHECK (status IN ('queued', 'processing', 'completed', 'failed', 'cancelled'))
);
CREATE INDEX IF NOT EXISTS guard_analysis_runs_tenant_status_idx
  ON guard_analysis_runs (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS guard_analysis_runs_source_idx
  ON guard_analysis_runs (source_ref_id, created_at DESC);

-- Requests are separate from analysis runs. Automatic discovery deduplicates
-- a source version, while a hospital user may explicitly request a new run of
-- the same immutable source version for audit or rule-version comparison.
CREATE TABLE IF NOT EXISTS guard_scan_requests (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  source_type VARCHAR(48) NOT NULL,
  source_id VARCHAR(128) NOT NULL,
  source_version VARCHAR(128) NOT NULL DEFAULT '',
  input_fingerprint VARCHAR(128) NOT NULL,
  trigger_source VARCHAR(32) NOT NULL,
  requested_by BIGINT,
  status VARCHAR(24) NOT NULL DEFAULT 'queued',
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  analysis_run_id UUID REFERENCES guard_analysis_runs(id),
  failure_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ,
  CONSTRAINT guard_scan_requests_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_scan_requests_status_check CHECK (status IN ('queued', 'processing', 'completed', 'failed', 'superseded')),
  CONSTRAINT guard_scan_requests_trigger_check CHECK (trigger_source IN ('auto', 'manual', 'backfill'))
);
CREATE INDEX IF NOT EXISTS guard_scan_requests_claim_idx
  ON guard_scan_requests (status, available_at, created_at)
  WHERE status = 'queued';
CREATE INDEX IF NOT EXISTS guard_scan_requests_tenant_source_idx
  ON guard_scan_requests (tenant_id, source_type, source_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS guard_scan_requests_auto_version_uq
  ON guard_scan_requests (tenant_id, source_type, source_id, input_fingerprint)
  WHERE trigger_source = 'auto';

CREATE TABLE IF NOT EXISTS guard_scan_cursors (
  cursor_key VARCHAR(128) PRIMARY KEY,
  last_seen_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS guard_machine_findings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  analysis_run_id UUID NOT NULL REFERENCES guard_analysis_runs(id),
  source_ref_id UUID NOT NULL REFERENCES guard_source_refs(id),
  subject VARCHAR(32) NOT NULL,
  candidate_rule_code VARCHAR(128) NOT NULL DEFAULT '',
  detector_type VARCHAR(32) NOT NULL,
  detector_output JSONB NOT NULL DEFAULT '{}'::jsonb,
  location_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_machine_findings_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_machine_findings_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_machine_findings_detector_check CHECK (detector_type IN ('keyword', 'structure', 'ocr', 'semantic_candidate', 'source_candidate'))
);
CREATE INDEX IF NOT EXISTS guard_machine_findings_tenant_time_idx
  ON guard_machine_findings (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS guard_machine_findings_run_idx
  ON guard_machine_findings (analysis_run_id, created_at ASC);

CREATE TABLE IF NOT EXISTS guard_findings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  subject VARCHAR(32) NOT NULL,
  source_ref_id UUID NOT NULL REFERENCES guard_source_refs(id),
  analysis_run_id UUID NOT NULL REFERENCES guard_analysis_runs(id),
  candidate_rule_code VARCHAR(128) NOT NULL DEFAULT '',
  risk_name VARCHAR(255) NOT NULL DEFAULT '',
  priority VARCHAR(24) NOT NULL DEFAULT 'review',
  status VARCHAR(24) NOT NULL DEFAULT 'in_review',
  fact_summary TEXT NOT NULL DEFAULT '',
  basis_slice TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_findings_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_findings_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_findings_priority_check CHECK (priority IN ('review', 'high', 'urgent')),
  CONSTRAINT guard_findings_status_check CHECK (status IN ('discovered', 'in_review', 'dismissed', 'indeterminate', 'linked_to_case'))
);
CREATE INDEX IF NOT EXISTS guard_findings_tenant_status_idx
  ON guard_findings (tenant_id, status, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS guard_findings_run_rule_uq
  ON guard_findings (analysis_run_id, candidate_rule_code)
  WHERE candidate_rule_code <> '';

CREATE TABLE IF NOT EXISTS guard_evidence (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  source_ref_id UUID NOT NULL REFERENCES guard_source_refs(id),
  source_snapshot_id UUID REFERENCES guard_source_snapshots(id),
  evidence_type VARCHAR(32) NOT NULL,
  location_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  fact_text TEXT NOT NULL DEFAULT '',
  immutable_digest VARCHAR(128) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_evidence_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_evidence_type_check CHECK (evidence_type IN ('transcript', 'frontdesk_event', 'medical_record', 'content_text', 'content_image'))
);
CREATE UNIQUE INDEX IF NOT EXISTS guard_evidence_digest_uq
  ON guard_evidence (tenant_id, immutable_digest);
CREATE INDEX IF NOT EXISTS guard_evidence_source_idx
  ON guard_evidence (tenant_id, source_ref_id, created_at DESC);

CREATE TABLE IF NOT EXISTS guard_finding_evidence (
  tenant_id BIGINT NOT NULL,
  finding_id UUID NOT NULL REFERENCES guard_findings(id) ON DELETE CASCADE,
  evidence_id UUID NOT NULL REFERENCES guard_evidence(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_finding_evidence_tenant_check CHECK (tenant_id > 0),
  PRIMARY KEY (finding_id, evidence_id)
);

CREATE TABLE IF NOT EXISTS guard_cases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  subject VARCHAR(32) NOT NULL,
  title VARCHAR(255) NOT NULL DEFAULT '',
  review_status VARCHAR(24) NOT NULL DEFAULT 'pending',
  conclusion VARCHAR(24) NOT NULL DEFAULT '',
  conclusion_reason TEXT NOT NULL DEFAULT '',
  responsible_employee_id BIGINT,
  responsible_department_id BIGINT,
  created_by BIGINT,
  reviewed_by BIGINT,
  reviewed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_cases_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_cases_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_cases_review_status_check CHECK (review_status IN ('pending', 'reviewing', 'confirmed', 'dismissed', 'indeterminate', 'closed')),
  CONSTRAINT guard_cases_conclusion_check CHECK (conclusion IN ('', 'confirmed', 'dismissed', 'indeterminate'))
);
CREATE INDEX IF NOT EXISTS guard_cases_tenant_status_idx
  ON guard_cases (tenant_id, review_status, created_at DESC);

CREATE TABLE IF NOT EXISTS guard_case_findings (
  tenant_id BIGINT NOT NULL,
  case_id UUID NOT NULL REFERENCES guard_cases(id) ON DELETE CASCADE,
  finding_id UUID NOT NULL REFERENCES guard_findings(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_case_findings_tenant_check CHECK (tenant_id > 0),
  PRIMARY KEY (case_id, finding_id)
);

CREATE TABLE IF NOT EXISTS guard_case_evidence (
  tenant_id BIGINT NOT NULL,
  case_id UUID NOT NULL REFERENCES guard_cases(id) ON DELETE CASCADE,
  evidence_id UUID NOT NULL REFERENCES guard_evidence(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_case_evidence_tenant_check CHECK (tenant_id > 0),
  PRIMARY KEY (case_id, evidence_id)
);

CREATE TABLE IF NOT EXISTS guard_review_actions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  target_type VARCHAR(32) NOT NULL,
  target_id UUID NOT NULL,
  action VARCHAR(32) NOT NULL,
  actor_id BIGINT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_review_actions_tenant_check CHECK (tenant_id > 0)
);
CREATE INDEX IF NOT EXISTS guard_review_actions_target_idx
  ON guard_review_actions (tenant_id, target_type, target_id, created_at ASC);

CREATE TABLE IF NOT EXISTS guard_remediation_tasks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  case_id UUID NOT NULL REFERENCES guard_cases(id),
  title VARCHAR(255) NOT NULL DEFAULT '',
  action_text TEXT NOT NULL DEFAULT '',
  assignee_employee_id BIGINT,
  assignee_department_id BIGINT,
  due_at TIMESTAMPTZ,
  status VARCHAR(24) NOT NULL DEFAULT 'open',
  feedback TEXT NOT NULL DEFAULT '',
  closed_by BIGINT,
  closed_at TIMESTAMPTZ,
  created_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT guard_remediation_tasks_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_remediation_tasks_status_check CHECK (status IN ('open', 'in_progress', 'submitted', 'closed', 'overdue'))
);
CREATE INDEX IF NOT EXISTS guard_remediation_tasks_tenant_status_idx
  ON guard_remediation_tasks (tenant_id, status, due_at);

CREATE TABLE IF NOT EXISTS guard_rule_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT,
  rule_code VARCHAR(128) NOT NULL,
  rule_name VARCHAR(255) NOT NULL DEFAULT '',
  subject VARCHAR(32) NOT NULL,
  source_kind VARCHAR(32) NOT NULL DEFAULT 'platform',
  version VARCHAR(64) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'draft',
  applicability JSONB NOT NULL DEFAULT '{}'::jsonb,
  basis_slice TEXT NOT NULL DEFAULT '',
  execution_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  published_at TIMESTAMPTZ,
  CONSTRAINT guard_rule_versions_tenant_check CHECK (tenant_id IS NULL OR tenant_id > 0),
  CONSTRAINT guard_rule_versions_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_rule_versions_source_check CHECK (source_kind IN ('platform', 'local')),
  CONSTRAINT guard_rule_versions_status_check CHECK (status IN ('draft', 'published', 'disabled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS guard_rule_versions_identity_uq
  ON guard_rule_versions (COALESCE(tenant_id, 0), rule_code, version);
CREATE INDEX IF NOT EXISTS guard_rule_versions_scope_idx
  ON guard_rule_versions (tenant_id, subject, status);

CREATE TABLE IF NOT EXISTS guard_local_standards (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  code VARCHAR(128) NOT NULL,
  name VARCHAR(255) NOT NULL DEFAULT '',
  standard_text TEXT NOT NULL DEFAULT '',
  subject VARCHAR(32) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'draft',
  version VARCHAR(64) NOT NULL DEFAULT '1',
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  published_at TIMESTAMPTZ,
  CONSTRAINT guard_local_standards_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_local_standards_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_local_standards_status_check CHECK (status IN ('draft', 'published', 'disabled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS guard_local_standards_identity_uq
  ON guard_local_standards (tenant_id, code, version);

CREATE TABLE IF NOT EXISTS guard_exports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  export_type VARCHAR(24) NOT NULL,
  target_type VARCHAR(24) NOT NULL,
  target_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
  snapshot_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  format VARCHAR(16) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'queued',
  requested_by BIGINT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ,
  failure_reason TEXT NOT NULL DEFAULT '',
  CONSTRAINT guard_exports_tenant_check CHECK (tenant_id > 0),
  CONSTRAINT guard_exports_format_check CHECK (format IN ('pdf', 'docx', 'xlsx')),
  CONSTRAINT guard_exports_status_check CHECK (status IN ('queued', 'processing', 'completed', 'failed'))
);
CREATE INDEX IF NOT EXISTS guard_exports_tenant_time_idx
  ON guard_exports (tenant_id, created_at DESC);

-- The first platform rules are executable candidates, not final compliance
-- determinations. Their source material remains platform-owned; only the
-- minimum basis slice is exposed with a finding.
INSERT INTO guard_rule_versions (
  tenant_id, rule_code, rule_name, subject, source_kind, version, status,
  applicability, basis_slice, execution_spec, created_at, published_at
) VALUES
  (
    NULL, 'communication.guarantee.direct', '诊疗效果保证性承诺', 'communication',
    'platform', '20260902.1', 'published',
    '{"business_scopes":["doctor","consultant","nurse","therapist"]}'::jsonb,
    '发现对诊疗结果作出保证性承诺的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"keyword","operator":"AND","keywords":["保证","治好"]}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.guarantee.absolute', '绝对化诊疗效果表述', 'communication',
    'platform', '20260902.1', 'published',
    '{"business_scopes":["doctor","consultant","nurse","therapist"]}'::jsonb,
    '发现绝对化表达与诊疗结果词同时出现的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"pattern","groups":[["一定","肯定","绝对","必然"],["能","会"],["治好","康复","恢复"]]}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.fear.exaggeration', '夸大风险推动决策', 'communication',
    'platform', '20260902.1', 'published',
    '{"business_scopes":["doctor","consultant","nurse","therapist"]}'::jsonb,
    '发现可能利用严重后果或时间压力推动患者决策的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"keyword","operator":"OR","keywords":["很严重","非常危险","再不做就来不及了","拖不得"]}'::jsonb,
    NOW(), NOW()
  )
ON CONFLICT DO NOTHING;
