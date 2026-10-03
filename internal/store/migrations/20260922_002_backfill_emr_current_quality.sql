-- 将历史质检批次回填为每份病历唯一的当前质检结果。
--
-- 只读取 emr_check_runs / emr_check_results，不修改历史数据。
-- 每份病历只选择最新一次质检批次；如果最新批次正在执行或执行失败，
-- 当前结果保留对应状态，当前问题明细为空。
--
-- 时间：2026-09-22

CREATE TEMP TABLE emr_quality_backfill_latest_runs ON COMMIT DROP AS
SELECT DISTINCT ON (record_id)
       id,
       tenant_id,
       record_id,
       snapshot_id,
       template_version_id,
       trigger_action,
       status,
       overall_result,
       started_at,
       completed_at,
       failure_reason
FROM public.emr_check_runs
ORDER BY record_id, started_at DESC, completed_at DESC NULLS LAST, id DESC;

WITH
result_counts AS (
  SELECT
    result.check_run_id,
    COUNT(*) FILTER (
      WHERE result.execution_status <> 'executor_not_implemented'
    )::int AS result_count,
    COUNT(*) FILTER (
      WHERE result.execution_status <> 'executor_not_implemented'
        AND (
          result.conclusion <> '符合'
          OR result.check_status <> '已完成'
          OR result.handling_result <> '允许继续'
        )
    )::int AS attention_count,
    COUNT(*) FILTER (
      WHERE result.execution_status <> 'executor_not_implemented'
        AND result.handling_result = '阻断'
    )::int AS blocking_count
  FROM public.emr_check_results result
  JOIN emr_quality_backfill_latest_runs run ON run.id = result.check_run_id
  GROUP BY result.check_run_id
)
INSERT INTO public.emr_record_quality_current
(
  record_id,
  tenant_id,
  snapshot_id,
  template_version_id,
  check_run_id,
  trigger_action,
  status,
  overall_result,
  started_at,
  completed_at,
  checked_at,
  failure_reason,
  result_count,
  attention_count,
  blocking_count,
  updated_at
)
SELECT
  run.record_id,
  run.tenant_id,
  run.snapshot_id,
  run.template_version_id,
  run.id,
  run.trigger_action,
  run.status,
  run.overall_result,
  run.started_at,
  run.completed_at,
  COALESCE(run.completed_at, run.started_at, NOW()),
  COALESCE(run.failure_reason, ''),
  COALESCE(counts.result_count, 0),
  COALESCE(counts.attention_count, 0),
  COALESCE(counts.blocking_count, 0),
  NOW()
FROM emr_quality_backfill_latest_runs run
LEFT JOIN result_counts counts ON counts.check_run_id = run.id
ON CONFLICT (record_id) DO UPDATE SET
  tenant_id = EXCLUDED.tenant_id,
  snapshot_id = EXCLUDED.snapshot_id,
  template_version_id = EXCLUDED.template_version_id,
  check_run_id = EXCLUDED.check_run_id,
  trigger_action = EXCLUDED.trigger_action,
  status = EXCLUDED.status,
  overall_result = EXCLUDED.overall_result,
  started_at = EXCLUDED.started_at,
  completed_at = EXCLUDED.completed_at,
  checked_at = EXCLUDED.checked_at,
  failure_reason = EXCLUDED.failure_reason,
  result_count = EXCLUDED.result_count,
  attention_count = EXCLUDED.attention_count,
  blocking_count = EXCLUDED.blocking_count,
  updated_at = NOW();

DELETE FROM public.emr_record_quality_current_items current_item
WHERE EXISTS (
  SELECT 1
  FROM public.emr_record_quality_current current_quality
  WHERE current_quality.record_id = current_item.record_id
);

INSERT INTO public.emr_record_quality_current_items
(
  tenant_id,
  record_id,
  check_run_id,
  result_id,
  quality_requirement_id,
  requirement_code,
  requirement_name,
  field_code,
  field_name,
  rule_code,
  rule_name,
  configured_execution_mode,
  actual_execution_mode,
  execution_status,
  conclusion,
  check_status,
  incomplete_reason,
  handling_result,
  meets_deadline,
  is_attention,
  is_blocking,
  evidence,
  hit_explanation,
  suggested_handling,
  display_message,
  sort_order,
  checked_at
)
SELECT
  run.tenant_id,
  run.record_id,
  result.check_run_id,
  result.id,
  result.quality_requirement_id,
  result.requirement_code,
  result.requirement_name,
  COALESCE(NULLIF(result.evidence->>'field_code', ''), ''),
  COALESCE(NULLIF(result.evidence->>'field_name', ''), ''),
  COALESCE(NULLIF(result.evidence->>'rule_code', ''), result.requirement_code),
  COALESCE(NULLIF(result.evidence->>'rule_name', ''), result.requirement_name),
  result.configured_execution_mode,
  result.actual_execution_mode,
  result.execution_status,
  result.conclusion,
  result.check_status,
  result.incomplete_reason,
  result.handling_result,
  result.meets_deadline,
  (
    result.conclusion <> '符合'
    OR result.check_status <> '已完成'
    OR result.handling_result <> '允许继续'
  ),
  (result.handling_result = '阻断'),
  result.evidence,
  result.hit_explanation,
  result.suggested_handling,
  COALESCE(
    NULLIF(result.hit_explanation, ''),
    NULLIF(result.incomplete_reason, ''),
    NULLIF(result.suggested_handling, ''),
    NULLIF(result.requirement_name, ''),
    result.requirement_code
  ),
  ROW_NUMBER() OVER (PARTITION BY run.record_id ORDER BY result.requirement_code, result.id)::int,
  COALESCE(run.completed_at, run.started_at, NOW())
FROM emr_quality_backfill_latest_runs run
JOIN public.emr_check_results result ON result.check_run_id = run.id
WHERE run.status = '已完成'
  AND result.execution_status <> 'executor_not_implemented'
ORDER BY run.record_id, result.requirement_code, result.id;
