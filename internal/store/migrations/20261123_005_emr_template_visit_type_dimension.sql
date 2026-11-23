-- 模板身份增加「就诊类型」维度：
-- 1) 唯一索引纳入 visit_type（current / version 两组）；
-- 2) 妇科、中医模板 code 归一（gyn_first/gyn_follow → gynecology，tcm_first/tcm_follow → tcm），保留 visit_type；
-- 3) 科室映射、病历来源引用同步。

DROP INDEX IF EXISTS emr_templates_scope_code_current_uq;
CREATE UNIQUE INDEX emr_templates_scope_code_current_uq
  ON public.emr_templates (COALESCE(tenant_id, 0), code, visit_type) WHERE is_current = true;

DROP INDEX IF EXISTS emr_templates_scope_code_version_uq;
CREATE UNIQUE INDEX emr_templates_scope_code_version_uq
  ON public.emr_templates (COALESCE(tenant_id, 0), code, version_no, visit_type);

DROP INDEX IF EXISTS emr_templates_tenant_code_version_uq;
CREATE UNIQUE INDEX emr_templates_tenant_code_version_uq
  ON public.emr_templates (tenant_id, code, version_no, visit_type);

UPDATE public.emr_templates SET code = 'gynecology', updated_at = NOW() WHERE code IN ('gyn_first', 'gyn_follow');
UPDATE public.emr_templates SET code = 'tcm', updated_at = NOW() WHERE code IN ('tcm_first', 'tcm_follow');

UPDATE public.departments SET template_code = 'gynecology' WHERE template_code IN ('gyn_first', 'gyn_follow');
UPDATE public.departments SET template_code = 'tcm' WHERE template_code IN ('tcm_first', 'tcm_follow');

UPDATE public.emr_records
   SET source_references_json = jsonb_set(source_references_json, ARRAY['template_code']::text[], to_jsonb('gynecology'::text))
 WHERE source_references_json->>'template_code' IN ('gyn_first', 'gyn_follow');
UPDATE public.emr_records
   SET source_references_json = jsonb_set(source_references_json, ARRAY['template_code']::text[], to_jsonb('tcm'::text))
 WHERE source_references_json->>'template_code' IN ('tcm_first', 'tcm_follow');
