-- 029：补齐科室 type（clinical / functional）
--
-- 027 批量开出的标准科室没写 type，导致门户按类型判断时显示成「自定义」；
-- 这里从平台字典回填，已有值不动。

UPDATE departments d
SET type = od.type, updated_at = NOW()
FROM organization_department_dict od
WHERE d.deleted_at IS NULL
  AND d.dict_code = od.code
  AND COALESCE(d.type, '') = '';
