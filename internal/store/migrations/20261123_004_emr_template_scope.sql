-- 病历模板显式层级：scope 由 tenant_id 自动派生（platform / tenant）。
-- 纯增量：不改历史数据，查询可直接 WHERE scope='platform'。
ALTER TABLE public.emr_templates ADD COLUMN IF NOT EXISTS scope VARCHAR(16)
  GENERATED ALWAYS AS (CASE WHEN COALESCE(tenant_id, 0) = 0 THEN 'platform' ELSE 'tenant' END) STORED;

CREATE INDEX IF NOT EXISTS emr_templates_scope_code_idx
  ON public.emr_templates (scope, code, status);
