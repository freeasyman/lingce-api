-- 024：departments 关联平台组织字典（dict_code）
--
-- 目的：机构部门引用平台字典，实现「标准 → 实例」。
-- 保留 departments.code（HIS 短码等历史值）不动，新增 dict_code 引用字典标准码。
-- 匹配不到字典的部门标记 is_custom（机构自定义 / 历史大部门）。

ALTER TABLE departments ADD COLUMN IF NOT EXISTS dict_code text;
ALTER TABLE departments ADD COLUMN IF NOT EXISTS is_custom boolean NOT NULL DEFAULT false;

-- 1) 按名称匹配字典
UPDATE departments d
SET dict_code = od.code, is_custom = false
FROM organization_department_dict od
WHERE d.name = od.name
  AND d.deleted_at IS NULL
  AND (d.dict_code IS NULL OR d.dict_code <> od.code);

-- 2) 按 legacy_codes（历史名称）匹配
UPDATE departments d
SET dict_code = od.code, is_custom = false
FROM organization_department_dict od
WHERE d.dict_code IS NULL
  AND d.deleted_at IS NULL
  AND d.name = ANY(od.legacy_codes);

-- 3) 匹配不到 → 标记自定义
UPDATE departments d
SET is_custom = true
WHERE d.dict_code IS NULL
  AND d.deleted_at IS NULL;
