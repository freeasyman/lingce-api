-- 当前质检结果改为每份病历一行、整包 JSON 保存。
--
-- emr_check_runs / emr_check_results 作为兼容和审计数据暂时保留；
-- 正式当前结果不再依赖 emr_record_quality_current_items。

ALTER TABLE public.emr_record_quality_current
  ADD COLUMN IF NOT EXISTS result_json JSONB NOT NULL DEFAULT
    '{"results":[]}'::jsonb;

WITH packaged AS (
  SELECT
    current_quality.record_id,
    jsonb_build_object(
      'status', current_quality.status,
      'overall_result', current_quality.overall_result,
      'trigger_action', current_quality.trigger_action,
      'snapshot_id', current_quality.snapshot_id,
      'template_version_id', current_quality.template_version_id,
      'check_run_id', current_quality.check_run_id,
      'started_at', current_quality.started_at,
      'completed_at', current_quality.completed_at,
      'checked_at', current_quality.checked_at,
      'failure_reason', current_quality.failure_reason,
      'result_count', current_quality.result_count,
      'attention_count', current_quality.attention_count,
      'blocking_count', current_quality.blocking_count,
      'results', COALESCE(
        jsonb_agg(
          jsonb_build_object(
            'id', item.result_id,
            'check_run_id', item.check_run_id,
            'quality_requirement_id', item.quality_requirement_id,
            'requirement_code', item.requirement_code,
            'requirement_name', item.requirement_name,
            'field_code', item.field_code,
            'field_name', item.field_name,
            'rule_code', item.rule_code,
            'rule_name', item.rule_name,
            'configured_execution_mode', item.configured_execution_mode,
            'actual_execution_mode', item.actual_execution_mode,
            'execution_status', item.execution_status,
            'conclusion', item.conclusion,
            'check_status', item.check_status,
            'incomplete_reason', item.incomplete_reason,
            'handling_result', item.handling_result,
            'meets_deadline', item.meets_deadline,
            'evidence', item.evidence,
            'hit_explanation', item.hit_explanation,
            'suggested_handling', item.suggested_handling
          )
          ORDER BY item.sort_order, item.id
        ) FILTER (WHERE item.id IS NOT NULL),
        '[]'::jsonb
      )
    ) AS result_json
  FROM public.emr_record_quality_current current_quality
  LEFT JOIN public.emr_record_quality_current_items item
    ON item.record_id = current_quality.record_id
  GROUP BY
    current_quality.record_id,
    current_quality.status,
    current_quality.overall_result,
    current_quality.trigger_action,
    current_quality.snapshot_id,
    current_quality.template_version_id,
    current_quality.check_run_id,
    current_quality.started_at,
    current_quality.completed_at,
    current_quality.checked_at,
    current_quality.failure_reason,
    current_quality.result_count,
    current_quality.attention_count,
    current_quality.blocking_count
)
UPDATE public.emr_record_quality_current current_quality
SET result_json = packaged.result_json,
    updated_at = NOW()
FROM packaged
WHERE packaged.record_id = current_quality.record_id;

COMMENT ON COLUMN public.emr_record_quality_current.result_json IS
  '当前病历完整质检结果包；正式读取和写入均以此字段为准。';

COMMENT ON TABLE public.emr_record_quality_current_items IS
  '历史兼容表，不再由正式当前质检链路读写。';
