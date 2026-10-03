-- 门诊病历评分器独立结果存储。
--
-- 这组表不复用旧质检结果表，当前阶段只承载程序评分结果；
-- 大模型评分、历史批次和正式业务入口在后续阶段接入。

CREATE TABLE IF NOT EXISTS public.emr_record_score_current (
  record_id UUID PRIMARY KEY REFERENCES public.emr_records(id) ON DELETE CASCADE,
  tenant_id BIGINT NOT NULL,
  snapshot_id UUID REFERENCES public.emr_record_snapshots(id) ON DELETE SET NULL,
  score_version VARCHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT '已完成',
  total_score INTEGER NOT NULL DEFAULT 0,
  grade VARCHAR(8) NOT NULL DEFAULT '丙级',
  program_score INTEGER NOT NULL DEFAULT 0,
  program_deduct_points INTEGER NOT NULL DEFAULT 0,
  major_program_score INTEGER NOT NULL DEFAULT 0,
  direct_grade VARCHAR(8) NOT NULL DEFAULT '',
  direct_grade_reason TEXT NOT NULL DEFAULT '',
  pending_sections JSONB NOT NULL DEFAULT '[]'::jsonb,
  failure_reason TEXT NOT NULL DEFAULT '',
  scored_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_record_score_current_status_check
    CHECK (status IN ('执行中', '已完成', '执行失败')),
  CONSTRAINT emr_record_score_current_grade_check
    CHECK (grade IN ('甲级', '乙级', '丙级', '')),
  CONSTRAINT emr_record_score_current_score_check
    CHECK (
      total_score BETWEEN 0 AND 100
      AND program_score BETWEEN 0 AND 100
      AND program_deduct_points BETWEEN 0 AND 100
      AND major_program_score BETWEEN 0 AND 100
    )
);

CREATE INDEX IF NOT EXISTS emr_record_score_current_tenant_time_idx
  ON public.emr_record_score_current (tenant_id, scored_at DESC);

CREATE INDEX IF NOT EXISTS emr_record_score_current_tenant_grade_idx
  ON public.emr_record_score_current (tenant_id, grade, scored_at DESC);

CREATE TABLE IF NOT EXISTS public.emr_record_score_current_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  record_id UUID NOT NULL REFERENCES public.emr_record_score_current(record_id) ON DELETE CASCADE,
  section_code VARCHAR(64) NOT NULL,
  section_name VARCHAR(64) NOT NULL,
  standard_score INTEGER NOT NULL,
  score INTEGER NOT NULL,
  program_deduct_points INTEGER NOT NULL DEFAULT 0,
  pending BOOLEAN NOT NULL DEFAULT FALSE,
  findings JSONB NOT NULL DEFAULT '[]'::jsonb,
  sort_order INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_record_score_current_items_score_check
    CHECK (
      standard_score >= 0
      AND score >= 0
      AND score <= standard_score
      AND program_deduct_points >= 0
      AND program_deduct_points <= standard_score
    )
);

CREATE INDEX IF NOT EXISTS emr_record_score_current_items_record_idx
  ON public.emr_record_score_current_items (record_id, sort_order, id);

CREATE INDEX IF NOT EXISTS emr_record_score_current_items_tenant_section_idx
  ON public.emr_record_score_current_items (tenant_id, section_code, updated_at DESC);
