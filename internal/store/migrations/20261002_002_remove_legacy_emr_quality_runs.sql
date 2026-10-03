-- 质检结果已统一保存到 emr_record_quality_current.result_json。
-- 删除旧的批次结果链路，不保留兼容读写。

DO $$
DECLARE
  dependency RECORD;
BEGIN
  FOR dependency IN
    SELECT DISTINCT
      ns.nspname AS schema_name,
      rel.relname AS table_name,
      con.conname AS constraint_name
    FROM pg_constraint con
    JOIN pg_class rel ON rel.oid = con.conrelid
    JOIN pg_namespace ns ON ns.oid = rel.relnamespace
    JOIN pg_class referenced ON referenced.oid = con.confrelid
    JOIN pg_namespace referenced_ns ON referenced_ns.oid = referenced.relnamespace
    WHERE con.contype = 'f'
      AND referenced_ns.nspname = 'public'
      AND referenced.relname IN ('emr_check_runs', 'emr_check_results')
  LOOP
    EXECUTE format(
      'ALTER TABLE %I.%I DROP CONSTRAINT %I',
      dependency.schema_name,
      dependency.table_name,
      dependency.constraint_name
    );
  END LOOP;
END $$;

ALTER TABLE IF EXISTS public.emr_record_quality_current
  DROP COLUMN IF EXISTS check_run_id;

ALTER TABLE IF EXISTS public.emr_process_records
  DROP COLUMN IF EXISTS check_run_id;

DROP TABLE IF EXISTS public.emr_check_results;
DROP TABLE IF EXISTS public.emr_check_runs;
