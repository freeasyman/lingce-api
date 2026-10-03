-- 补齐当前 EMR 模板与规则配置表的正式迁移。
--
-- 这些表此前由手工 SQL 建立，但已被当前模板、质检和病历详情链路使用。
-- 本迁移采用幂等写法：已有表保留数据，只补齐当前代码依赖的字段、约束和索引。

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS public.emr_fields (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL DEFAULT 0,
  code VARCHAR(128) NOT NULL,
  name VARCHAR(255) NOT NULL,
  specialty VARCHAR(64),
  content_category VARCHAR(32) NOT NULL DEFAULT '',
  input_type VARCHAR(32) NOT NULL DEFAULT '长文本',
  description TEXT NOT NULL DEFAULT '',
  options_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  structure_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  quality_rules JSONB NOT NULL DEFAULT '[]'::jsonb,
  status VARCHAR(16) NOT NULL DEFAULT 'enabled',
  created_by BIGINT NOT NULL DEFAULT 0,
  updated_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS code VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS specialty VARCHAR(64);
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS content_category VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS input_type VARCHAR(32) NOT NULL DEFAULT '长文本';
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS options_json JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS structure_json JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS quality_rules JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS status VARCHAR(16) NOT NULL DEFAULT 'enabled';
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS updated_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE public.emr_fields ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS emr_fields_scope_code_uq
  ON public.emr_fields (tenant_id, code);
CREATE INDEX IF NOT EXISTS emr_fields_scope_status_idx
  ON public.emr_fields (tenant_id, status, specialty, code);

CREATE TABLE IF NOT EXISTS public.emr_template_fields (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL DEFAULT 0,
  template_id UUID NOT NULL REFERENCES public.emr_templates(id) ON DELETE CASCADE,
  field_id UUID NOT NULL REFERENCES public.emr_fields(id) ON DELETE RESTRICT,
  display_order INTEGER NOT NULL DEFAULT 0,
  is_visible BOOLEAN NOT NULL DEFAULT TRUE,
  is_required BOOLEAN NOT NULL DEFAULT FALSE,
  description TEXT NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS template_id UUID;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS field_id UUID;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS display_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS is_visible BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS is_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE public.emr_template_fields ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS emr_template_fields_template_field_uq
  ON public.emr_template_fields (template_id, field_id);
CREATE INDEX IF NOT EXISTS emr_template_fields_template_order_idx
  ON public.emr_template_fields (template_id, display_order, id);

CREATE TABLE IF NOT EXISTS public.emr_quality_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL DEFAULT 0,
  code VARCHAR(160) NOT NULL,
  name VARCHAR(255) NOT NULL,
  rule_type VARCHAR(32) NOT NULL DEFAULT '缺项',
  quality_group VARCHAR(128) NOT NULL DEFAULT '',
  source_name VARCHAR(255) NOT NULL DEFAULT '',
  source_version TEXT NOT NULL DEFAULT '',
  evaluated_fact TEXT NOT NULL DEFAULT '',
  pass_condition TEXT NOT NULL DEFAULT '',
  precondition TEXT NOT NULL DEFAULT '',
  evidence_basis TEXT NOT NULL DEFAULT '',
  execution_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
  status VARCHAR(16) NOT NULL DEFAULT 'published',
  created_by BIGINT NOT NULL DEFAULT 0,
  published_at TIMESTAMPTZ,
  published_by BIGINT,
  disabled_at TIMESTAMPTZ,
  disabled_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS code VARCHAR(160) NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS rule_type VARCHAR(32) NOT NULL DEFAULT '缺项';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS quality_group VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS source_name VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS source_version TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS evaluated_fact TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS pass_condition TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS precondition TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS evidence_basis TEXT NOT NULL DEFAULT '';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS execution_spec JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS status VARCHAR(16) NOT NULL DEFAULT 'published';
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS published_by BIGINT;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS disabled_by BIGINT;
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE public.emr_quality_rules ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS emr_quality_rules_scope_code_uq
  ON public.emr_quality_rules (tenant_id, code);
CREATE INDEX IF NOT EXISTS emr_quality_rules_scope_status_idx
  ON public.emr_quality_rules (tenant_id, status, quality_group, code);

CREATE TABLE IF NOT EXISTS public.emr_template_quality_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL DEFAULT 0,
  template_id UUID NOT NULL REFERENCES public.emr_templates(id) ON DELETE CASCADE,
  quality_rule_id UUID NOT NULL REFERENCES public.emr_quality_rules(id) ON DELETE RESTRICT,
  is_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  execution_mode VARCHAR(16) NOT NULL DEFAULT '程序判断',
  deadline_action VARCHAR(24) NOT NULL DEFAULT '仅提示',
  display_order INTEGER NOT NULL DEFAULT 0,
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS template_id UUID;
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS quality_rule_id UUID;
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS is_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS execution_mode VARCHAR(16) NOT NULL DEFAULT '程序判断';
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS deadline_action VARCHAR(24) NOT NULL DEFAULT '仅提示';
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS display_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS created_by BIGINT NOT NULL DEFAULT 0;
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE public.emr_template_quality_rules ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS emr_template_quality_rules_template_rule_uq
  ON public.emr_template_quality_rules (template_id, quality_rule_id);
CREATE INDEX IF NOT EXISTS emr_template_quality_rules_template_order_idx
  ON public.emr_template_quality_rules (template_id, is_enabled, display_order, id);

-- 只清理已确认没有现行 Go 代码引用的历史 EMR 模型。
DROP TABLE IF EXISTS public.emr_record_quality_current_items;
DROP TABLE IF EXISTS public.emr_score_rules;
DROP TABLE IF EXISTS public.emr_ai_candidates;
DROP TABLE IF EXISTS public.emr_template_quality_requirements;
DROP TABLE IF EXISTS public.emr_template_sections;
DROP TABLE IF EXISTS public.emr_template_versions;
DROP TABLE IF EXISTS public.emr_quality_requirements;
DROP TABLE IF EXISTS public.emr_document_versions;
DROP TABLE IF EXISTS public.emr_outpatient_records;
DROP TABLE IF EXISTS public.emr_rule_doctor_actions;
DROP TABLE IF EXISTS public.emr_rule_evidences;
DROP TABLE IF EXISTS public.emr_rule_summaries;
DROP TABLE IF EXISTS public.emr_rule_hits;
DROP TABLE IF EXISTS public.emr_rule_runs;
DROP TABLE IF EXISTS public.emr_template_rule_bindings;

