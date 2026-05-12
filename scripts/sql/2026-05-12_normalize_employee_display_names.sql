-- 统一员工展示姓名
-- 时间: 2026-05-12
-- 目的:
-- 1. 将 name/full_name 同时为 unknown、但 phone 可用的员工归一化到 phone
-- 2. 保持 name 与 full_name 同步，避免不同模块显示不一致

-- 预览将被修复的员工
SELECT id,
       tenant_id,
       COALESCE(NULLIF(name, ''), '<empty>') AS current_name,
       COALESCE(NULLIF(full_name, ''), '<empty>') AS current_full_name,
       phone
FROM employees
WHERE deleted_at IS NULL
  AND NULLIF(phone, '') IS NOT NULL
  AND lower(trim(COALESCE(name, ''))) = 'unknown'
  AND lower(trim(COALESCE(full_name, ''))) = 'unknown'
ORDER BY tenant_id, id;

-- 执行修复
UPDATE employees
SET full_name = phone,
    name = phone,
    updated_at = NOW()
WHERE deleted_at IS NULL
  AND NULLIF(phone, '') IS NOT NULL
  AND lower(trim(COALESCE(name, ''))) = 'unknown'
  AND lower(trim(COALESCE(full_name, ''))) = 'unknown';
