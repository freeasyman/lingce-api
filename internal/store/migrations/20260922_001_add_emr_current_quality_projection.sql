-- 当前质检结果投影表。
--
-- 目标：一份病历永远只有一份当前质检结果，用于病历详情页和质量管理页。
-- 历史批次表 emr_check_runs / emr_check_results 暂不删除，第一阶段仍保留兼容。
-- 后续如果普通保存质检不再保存历史批次，check_run_id/result_id 可以为空。
--
-- 署名：Codex
-- 时间：2026-09-22

CREATE TABLE IF NOT EXISTS public.emr_record_quality_current (
  record_id UUID PRIMARY KEY REFERENCES public.emr_records(id) ON DELETE CASCADE,
  tenant_id BIGINT NOT NULL,
  snapshot_id UUID REFERENCES public.emr_record_snapshots(id) ON DELETE SET NULL,
  template_version_id UUID,
  check_run_id UUID REFERENCES public.emr_check_runs(id) ON DELETE SET NULL,
  trigger_action VARCHAR(16) NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT '执行中',
  overall_result VARCHAR(16) NOT NULL DEFAULT '无法完成',
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  failure_reason TEXT NOT NULL DEFAULT '',
  result_count INTEGER NOT NULL DEFAULT 0,
  attention_count INTEGER NOT NULL DEFAULT 0,
  blocking_count INTEGER NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_record_quality_current_status_check
    CHECK (status IN ('执行中', '已完成', '执行失败')),
  CONSTRAINT emr_record_quality_current_result_check
    CHECK (overall_result IN ('无问题', '存在提示', '需要处理', '无法完成')),
  CONSTRAINT emr_record_quality_current_counts_check
    CHECK (result_count >= 0 AND attention_count >= 0 AND blocking_count >= 0)
);

CREATE INDEX IF NOT EXISTS emr_record_quality_current_tenant_checked_idx
  ON public.emr_record_quality_current (tenant_id, checked_at DESC);

CREATE INDEX IF NOT EXISTS emr_record_quality_current_tenant_result_idx
  ON public.emr_record_quality_current (tenant_id, overall_result, status, checked_at DESC);

CREATE INDEX IF NOT EXISTS emr_record_quality_current_snapshot_idx
  ON public.emr_record_quality_current (snapshot_id)
  WHERE snapshot_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS public.emr_record_quality_current_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT NOT NULL,
  record_id UUID NOT NULL REFERENCES public.emr_record_quality_current(record_id) ON DELETE CASCADE,
  check_run_id UUID REFERENCES public.emr_check_runs(id) ON DELETE SET NULL,
  result_id UUID REFERENCES public.emr_check_results(id) ON DELETE SET NULL,
  quality_requirement_id UUID,
  requirement_code VARCHAR(160) NOT NULL DEFAULT '',
  requirement_name VARCHAR(255) NOT NULL DEFAULT '',
  field_code VARCHAR(128) NOT NULL DEFAULT '',
  field_name VARCHAR(128) NOT NULL DEFAULT '',
  rule_code VARCHAR(160) NOT NULL DEFAULT '',
  rule_name VARCHAR(255) NOT NULL DEFAULT '',
  configured_execution_mode VARCHAR(16) NOT NULL DEFAULT '',
  actual_execution_mode VARCHAR(16) NOT NULL DEFAULT '',
  execution_status VARCHAR(32) NOT NULL DEFAULT 'completed',
  conclusion VARCHAR(16) NOT NULL DEFAULT '',
  check_status VARCHAR(16) NOT NULL DEFAULT '',
  incomplete_reason VARCHAR(32) NOT NULL DEFAULT '',
  handling_result VARCHAR(32) NOT NULL DEFAULT '',
  meets_deadline BOOLEAN,
  is_attention BOOLEAN NOT NULL DEFAULT FALSE,
  is_blocking BOOLEAN NOT NULL DEFAULT FALSE,
  evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
  hit_explanation TEXT NOT NULL DEFAULT '',
  suggested_handling TEXT NOT NULL DEFAULT '',
  display_message TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS emr_record_quality_current_items_record_idx
  ON public.emr_record_quality_current_items (record_id, sort_order, id);

CREATE INDEX IF NOT EXISTS emr_record_quality_current_items_attention_idx
  ON public.emr_record_quality_current_items (tenant_id, is_attention, is_blocking, checked_at DESC);

CREATE INDEX IF NOT EXISTS emr_record_quality_current_items_group_idx
  ON public.emr_record_quality_current_items (tenant_id, rule_code, field_code, is_attention, checked_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS emr_record_quality_current_items_result_uq
  ON public.emr_record_quality_current_items (record_id, result_id)
  WHERE result_id IS NOT NULL;
