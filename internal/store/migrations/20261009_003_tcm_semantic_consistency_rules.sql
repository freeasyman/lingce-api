BEGIN;
UPDATE public.emr_quality_rule_definitions
SET executor_kind='llm',
    action_policy=jsonb_build_object(
      'save', jsonb_build_object('enabled', false, 'blocking', false),
      'complete', jsonb_build_object('enabled', true, 'blocking', false),
      'archive', jsonb_build_object('enabled', true, 'blocking', false)
    ),
    evaluation_spec=evaluation_spec || '{"semantic":true}'::jsonb,
    updated_at=NOW()
WHERE version='2026-10-08-v1' AND rule_code IN ('TCM-DX-002','TCM-DX-003');
COMMIT;
