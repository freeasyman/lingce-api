-- 旧 108 条质检体系归零：删除旧质检结果、规则目录、模板绑定和栏位内规则配置。
-- 独立门诊百分制评分链路不在本迁移范围内。

DO $$
DECLARE
  dependency RECORD;
BEGIN
  FOR dependency IN
    SELECT ns.nspname AS schema_name,
           rel.relname AS table_name,
           con.conname AS constraint_name
    FROM pg_constraint con
    JOIN pg_class rel ON rel.oid = con.conrelid
    JOIN pg_namespace ns ON ns.oid = rel.relnamespace
    JOIN pg_class referenced ON referenced.oid = con.confrelid
    JOIN pg_namespace referenced_ns ON referenced_ns.oid = referenced.relnamespace
    WHERE con.contype = 'f'
      AND referenced_ns.nspname = 'public'
      AND referenced.relname IN (
        'emr_record_quality_current',
        'emr_check_runs',
        'emr_check_results',
        'emr_quality_rules',
        'emr_template_quality_rules',
        'emr_quality_requirements',
        'emr_template_quality_requirements'
      )
  LOOP
    EXECUTE format(
      'ALTER TABLE %I.%I DROP CONSTRAINT IF EXISTS %I',
      dependency.schema_name,
      dependency.table_name,
      dependency.constraint_name
    );
  END LOOP;
END $$;

DROP TABLE IF EXISTS public.emr_check_results;
DROP TABLE IF EXISTS public.emr_check_runs;
DROP TABLE IF EXISTS public.emr_record_quality_current;
DROP TABLE IF EXISTS public.emr_template_quality_rules;
DROP TABLE IF EXISTS public.emr_quality_requirements;
DROP TABLE IF EXISTS public.emr_template_quality_requirements;
DROP TABLE IF EXISTS public.emr_quality_rules;

ALTER TABLE IF EXISTS public.emr_fields
  DROP COLUMN IF EXISTS quality_rules;

DELETE FROM public.recording_analysis_prompts
WHERE code = 'emr_quality_check_v1';
