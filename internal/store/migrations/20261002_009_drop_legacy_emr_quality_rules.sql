-- 栏位规则已经是唯一运行时配置来源：emr_fields.quality_rules。
-- 旧规则目录及模板规则绑定不再被当前代码读取。

DO $$
DECLARE
  dependency RECORD;
BEGIN
  IF to_regclass('public.emr_quality_rules') IS NULL THEN
    RETURN;
  END IF;

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
      AND referenced.relname = 'emr_quality_rules'
  LOOP
    RAISE EXCEPTION '不能删除 emr_quality_rules：仍有外键依赖 %.% constraint %',
      dependency.schema_name, dependency.table_name, dependency.constraint_name;
  END LOOP;
END $$;

DROP TABLE IF EXISTS public.emr_quality_rules;
