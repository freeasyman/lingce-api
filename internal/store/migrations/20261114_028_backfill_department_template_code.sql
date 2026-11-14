-- 028：科室默认模板 code 从平台字典带出
--
-- 字典 template_code 作为「默认模板」：机构科室 template_code 为空时补上；
-- 已有值（机构自行覆盖的专科模板）一律不动。

UPDATE departments d
SET template_code = od.template_code, updated_at = NOW()
FROM organization_department_dict od
WHERE d.deleted_at IS NULL
  AND d.dict_code = od.code
  AND od.template_code IS NOT NULL
  AND COALESCE(d.template_code, '') = '';
