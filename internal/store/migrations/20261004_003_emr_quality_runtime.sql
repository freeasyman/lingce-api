-- 统一门诊病历质量体系第一版运行时结构。
-- 规则定义是唯一来源；模板栏位只提供输入结构，不存放质量规则。

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS public.emr_quality_rule_definitions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  rule_code VARCHAR(64) NOT NULL,
  version VARCHAR(32) NOT NULL,
  section_code VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  source_text TEXT NOT NULL DEFAULT '',
  product_text TEXT NOT NULL DEFAULT '',
  standard_points INTEGER NOT NULL DEFAULT 0,
  executor_kind VARCHAR(16) NOT NULL,
  input_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
  evaluation_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
  action_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
  message_templates JSONB NOT NULL DEFAULT '{}'::jsonb,
  status VARCHAR(16) NOT NULL DEFAULT 'draft',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_quality_rule_definitions_executor_check
    CHECK (executor_kind IN ('program', 'llm', 'hybrid', 'manual')),
  CONSTRAINT emr_quality_rule_definitions_status_check
    CHECK (status IN ('draft', 'published', 'retired')),
  CONSTRAINT emr_quality_rule_definitions_points_check
    CHECK (standard_points >= 0 AND standard_points <= 100)
);

CREATE UNIQUE INDEX IF NOT EXISTS emr_quality_rule_definitions_code_version_uq
  ON public.emr_quality_rule_definitions (rule_code, version);
CREATE INDEX IF NOT EXISTS emr_quality_rule_definitions_published_idx
  ON public.emr_quality_rule_definitions (status, section_code, rule_code);

CREATE TABLE IF NOT EXISTS public.emr_quality_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  record_id UUID NOT NULL REFERENCES public.emr_records(id) ON DELETE CASCADE,
  snapshot_id UUID REFERENCES public.emr_record_snapshots(id) ON DELETE SET NULL,
  action VARCHAR(16) NOT NULL,
  rule_set_version VARCHAR(32) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'running',
  overall_result VARCHAR(16) NOT NULL DEFAULT 'no_problem',
  total_score INTEGER NOT NULL DEFAULT 0,
  grade VARCHAR(8) NOT NULL DEFAULT '',
  major_score INTEGER NOT NULL DEFAULT 0,
  blocking_count INTEGER NOT NULL DEFAULT 0,
  pending_count INTEGER NOT NULL DEFAULT 0,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ,
  failure_reason TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_quality_runs_action_check
    CHECK (action IN ('save', 'complete', 'archive')),
  CONSTRAINT emr_quality_runs_status_check
    CHECK (status IN ('running', 'completed', 'failed')),
  CONSTRAINT emr_quality_runs_result_check
    CHECK (overall_result IN ('no_problem', 'has_problem', 'blocked', 'partial', 'failed')),
  CONSTRAINT emr_quality_runs_score_check
    CHECK (total_score BETWEEN 0 AND 100 AND major_score BETWEEN 0 AND 80)
);

CREATE INDEX IF NOT EXISTS emr_quality_runs_record_time_idx
  ON public.emr_quality_runs (record_id, started_at DESC);
CREATE INDEX IF NOT EXISTS emr_quality_runs_tenant_result_idx
  ON public.emr_quality_runs (tenant_id, overall_result, started_at DESC);

CREATE TABLE IF NOT EXISTS public.emr_quality_results (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id UUID NOT NULL REFERENCES public.emr_quality_runs(id) ON DELETE CASCADE,
  rule_id UUID NOT NULL REFERENCES public.emr_quality_rule_definitions(id) ON DELETE RESTRICT,
  rule_code VARCHAR(64) NOT NULL,
  section_code VARCHAR(64) NOT NULL,
  result VARCHAR(16) NOT NULL,
  execution_kind VARCHAR(16) NOT NULL,
  evidence JSONB NOT NULL DEFAULT '[]'::jsonb,
  reason TEXT NOT NULL DEFAULT '',
  deduction INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL DEFAULT '',
  blocking BOOLEAN NOT NULL DEFAULT FALSE,
  manual_review BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_quality_results_result_check
    CHECK (result IN ('pass', 'problem', 'not_evaluable')),
  CONSTRAINT emr_quality_results_execution_check
    CHECK (execution_kind IN ('program', 'llm', 'hybrid', 'manual')),
  CONSTRAINT emr_quality_results_deduction_check
    CHECK (deduction >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS emr_quality_results_run_rule_uq
  ON public.emr_quality_results (run_id, rule_code);
CREATE INDEX IF NOT EXISTS emr_quality_results_rule_result_idx
  ON public.emr_quality_results (rule_code, result, created_at DESC);

INSERT INTO public.emr_quality_rule_definitions
  (rule_code, version, section_code, name, source_text, product_text,
   standard_points, executor_kind, input_spec, evaluation_spec, action_policy,
   message_templates, status)
VALUES
  ('GEN-001', '2026-10-04', 'general', '各项医疗信息未填写一项',
   '各项医疗信息未填写一项', '', 2, 'program',
   '{"paths":["record.patient_snapshot"]}'::jsonb,
   '{"required_fields":["name","gender","birth_date"],"prompt_fields":["ethnicity","marital_status","occupation","work_unit","address"]}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking_fields":["name","gender","birth_date"]},"archive":{"enabled":true,"blocking_fields":["name","gender","birth_date"]}}'::jsonb,
   '{"save":"请填写{field}。","complete":"请补充{field}，再完成病历。","archive":"没有填写{field}，不能归档。"}'::jsonb,
   'published'),
  ('CC-001', '2026-10-04', 'chief_complaint', '缺少主诉内容',
   '缺少主诉内容', '', 5, 'program',
   '{"paths":["record.content.chief_complaint"]}'::jsonb,
   '{}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
   '{"save":"请填写主诉。","complete":"没有主诉，不能完成病历。","archive":"没有主诉，不能归档。"}'::jsonb,
   'published'),
  ('CC-005', '2026-10-04', 'chief_complaint', '主诉不超过20个字',
   '', '主诉不超过20个字', 0, 'program',
   '{"paths":["record.content.chief_complaint"]}'::jsonb,
   '{"max_length":20,"count_unit":"字符"}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
   '{"save":"主诉超过20个字，请精简。","complete":"主诉超过20个字，不能完成病历。","archive":"主诉超过20个字，不能归档。"}'::jsonb,
   'published'),
  ('HIS-001', '2026-10-04', 'history', '缺现病史',
   '缺现病史', '', 20, 'program',
   '{"paths":["record.content.present_illness"]}'::jsonb,
   '{}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
   '{"save":"请填写现病史。","complete":"没有现病史，不能完成病历。","archive":"没有现病史，不能归档。"}'::jsonb,
   'published'),
  ('HIS-009', '2026-10-04', 'history', '病史中的过敏史没有明确记录',
   '缺少与本次疾病相关的既往史及其他病史记录', '病史中的过敏史必须明确记录；没有过敏史也要明确填写。', 5, 'program',
   '{"paths":["record.content.allergy_history"]}'::jsonb,
   '{"accepted_negative":["无过敏史","无药物过敏史","否认过敏史"],"uncertain":["待确认","尚未询问","不详"]}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
   '{"save":"请明确记录过敏史；没有过敏史也请明确填写。","complete":"没有明确记录过敏史，不能完成病历。","archive":"没有明确记录过敏史，不能归档。"}'::jsonb,
   'published'),
  ('EXAM-001', '2026-10-04', 'physical_exam', '缺少必要的体检项目',
   '缺少必要的体检项目', '', 20, 'program',
   '{"paths":["record.content.physical_exam"]}'::jsonb,
   '{"mode":"whole_section_empty_only","required_config":"pending"}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
   '{"save":"没有记录体格检查，请补充体格检查。","complete":"没有记录体格检查，不能完成病历。","archive":"没有记录体格检查，不能归档。"}'::jsonb,
   'published'),
  ('DX-001', '2026-10-04', 'diagnosis', '未形成明确诊断，且没有进一步检查意见和建议',
   '未明确诊断病历，没有进一步检查意见和建议', '', 10, 'program',
   '{"paths":["record.content.diagnosis","record.content.auxiliary_exam","record.content.disposition","record.content.medical_advice"]}'::jsonb,
   '{"mode":"diagnosis_presence_only","semantic_check":"pending_llm"}'::jsonb,
   '{"save":{"enabled":true},"complete":{"enabled":true,"blocking":true},"archive":{"enabled":true,"blocking":true}}'::jsonb,
   '{"save":"请填写诊断。","complete":"没有形成明确诊断，不能完成病历。","archive":"没有形成明确诊断，不能归档。"}'::jsonb,
   'published')
ON CONFLICT (rule_code, version) DO UPDATE SET
  section_code=EXCLUDED.section_code,
  name=EXCLUDED.name,
  source_text=EXCLUDED.source_text,
  product_text=EXCLUDED.product_text,
  standard_points=EXCLUDED.standard_points,
  executor_kind=EXCLUDED.executor_kind,
  input_spec=EXCLUDED.input_spec,
  evaluation_spec=EXCLUDED.evaluation_spec,
  action_policy=EXCLUDED.action_policy,
  message_templates=EXCLUDED.message_templates,
  status=EXCLUDED.status,
  updated_at=NOW();

CREATE OR REPLACE VIEW public.emr_quality_published_rule_set AS
SELECT *
FROM public.emr_quality_rule_definitions
WHERE status='published';
