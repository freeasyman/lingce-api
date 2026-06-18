-- 角色真源统一审计脚本
-- 时间: 2026-05-11
-- 目的:
-- 1. 找出 inst_employee_roles 缺失但 institution_employee_roles 有值的员工
-- 2. 找出双表角色不一致的员工
-- 3. 找出 inst_employee_roles 多角色员工
-- 4. 找出活跃员工当前没有任何 inst_employee_roles 的情况
--
-- 使用方式:
-- 1. 先逐段执行 SELECT
-- 2. 保存结果作为断路前核对清单
-- 3. 不要在本脚本里直接改数据

-- 0. 统一抽取“当前角色”
-- legacy_current: 以 inst_employee_roles.created_at DESC 作为当前角色
-- new_current:    以 institution_employee_roles.created_at DESC 作为当前角色
WITH legacy_current AS (
    SELECT DISTINCT ON (er.tenant_id, er.employee_id)
           er.tenant_id,
           er.employee_id,
           lower(trim(er.role_code)) AS role_code,
           er.source,
           er.created_at
    FROM inst_employee_roles er
    WHERE er.employee_id IS NOT NULL
    ORDER BY er.tenant_id, er.employee_id, er.created_at DESC, er.role_code DESC
),
new_current AS (
    SELECT DISTINCT ON (e.tenant_id, ier.employee_id)
           e.tenant_id,
           ier.employee_id,
           lower(trim(ir.code)) AS role_code,
           ier.created_at
    FROM institution_employee_roles ier
    JOIN employees e ON e.id = ier.employee_id AND e.deleted_at IS NULL
    JOIN institution_roles ir ON ir.id = ier.role_id AND ir.deleted_at IS NULL
    ORDER BY e.tenant_id, ier.employee_id, ier.created_at DESC, ir.id DESC
)
SELECT 1;

-- 1. inst_employee_roles 缺失，但 institution_employee_roles 有值
WITH legacy_current AS (
    SELECT DISTINCT ON (er.tenant_id, er.employee_id)
           er.tenant_id,
           er.employee_id,
           lower(trim(er.role_code)) AS role_code,
           er.source,
           er.created_at
    FROM inst_employee_roles er
    WHERE er.employee_id IS NOT NULL
    ORDER BY er.tenant_id, er.employee_id, er.created_at DESC, er.role_code DESC
),
new_current AS (
    SELECT DISTINCT ON (e.tenant_id, ier.employee_id)
           e.tenant_id,
           ier.employee_id,
           lower(trim(ir.code)) AS role_code,
           ier.created_at
    FROM institution_employee_roles ier
    JOIN employees e ON e.id = ier.employee_id AND e.deleted_at IS NULL
    JOIN institution_roles ir ON ir.id = ier.role_id AND ir.deleted_at IS NULL
    ORDER BY e.tenant_id, ier.employee_id, ier.created_at DESC, ir.id DESC
)
SELECT e.tenant_id,
       e.id AS employee_id,
       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, e.phone, '-') AS employee_name,
       n.role_code AS institution_role_code,
       n.created_at AS institution_role_created_at
FROM employees e
JOIN new_current n
  ON n.tenant_id = e.tenant_id
 AND n.employee_id = e.id
LEFT JOIN legacy_current l
  ON l.tenant_id = e.tenant_id
 AND l.employee_id = e.id
WHERE e.deleted_at IS NULL
  AND l.employee_id IS NULL
ORDER BY e.tenant_id, e.id;

-- 2. 双表都有值，但当前角色不一致
WITH legacy_current AS (
    SELECT DISTINCT ON (er.tenant_id, er.employee_id)
           er.tenant_id,
           er.employee_id,
           lower(trim(er.role_code)) AS role_code,
           er.source,
           er.created_at
    FROM inst_employee_roles er
    WHERE er.employee_id IS NOT NULL
    ORDER BY er.tenant_id, er.employee_id, er.created_at DESC, er.role_code DESC
),
new_current AS (
    SELECT DISTINCT ON (e.tenant_id, ier.employee_id)
           e.tenant_id,
           ier.employee_id,
           lower(trim(ir.code)) AS role_code,
           ier.created_at
    FROM institution_employee_roles ier
    JOIN employees e ON e.id = ier.employee_id AND e.deleted_at IS NULL
    JOIN institution_roles ir ON ir.id = ier.role_id AND ir.deleted_at IS NULL
    ORDER BY e.tenant_id, ier.employee_id, ier.created_at DESC, ir.id DESC
)
SELECT e.tenant_id,
       e.id AS employee_id,
       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, e.phone, '-') AS employee_name,
       l.role_code AS legacy_role_code,
       l.source AS legacy_role_source,
       l.created_at AS legacy_role_created_at,
       n.role_code AS institution_role_code,
       n.created_at AS institution_role_created_at
FROM employees e
JOIN legacy_current l
  ON l.tenant_id = e.tenant_id
 AND l.employee_id = e.id
JOIN new_current n
  ON n.tenant_id = e.tenant_id
 AND n.employee_id = e.id
WHERE e.deleted_at IS NULL
  AND l.role_code <> n.role_code
ORDER BY e.tenant_id, e.id;

-- 3. inst_employee_roles 多角色员工
SELECT er.tenant_id,
       er.employee_id,
       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, e.phone, '-') AS employee_name,
       COUNT(*) AS role_row_count,
       COUNT(DISTINCT lower(trim(er.role_code))) AS distinct_role_count,
       STRING_AGG(DISTINCT lower(trim(er.role_code)), ', ' ORDER BY lower(trim(er.role_code))) AS role_codes,
       STRING_AGG(DISTINCT COALESCE(er.source, ''), ', ' ORDER BY COALESCE(er.source, '')) AS sources
FROM inst_employee_roles er
JOIN employees e ON e.id = er.employee_id AND e.deleted_at IS NULL
GROUP BY er.tenant_id, er.employee_id, e.full_name, e.name, e.username, e.phone
HAVING COUNT(DISTINCT lower(trim(er.role_code))) > 1
ORDER BY er.tenant_id, er.employee_id;

-- 4. 活跃员工没有任何 inst_employee_roles
SELECT e.tenant_id,
       e.id AS employee_id,
       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, e.phone, '-') AS employee_name,
       e.department_id,
       e.created_at
FROM employees e
LEFT JOIN inst_employee_roles er
  ON er.tenant_id = e.tenant_id
 AND er.employee_id = e.id
WHERE e.deleted_at IS NULL
  AND e.is_active::text IN ('1', 't', 'true', 'TRUE')
  AND er.employee_id IS NULL
ORDER BY e.tenant_id, e.id;

-- 5. institution_employee_roles 当前指向的 role_id 无法映射到可用 institution_roles
SELECT e.tenant_id,
       ier.employee_id,
       COALESCE(NULLIF(e.full_name, ''), NULLIF(e.name, ''), e.username, e.phone, '-') AS employee_name,
       ier.role_id,
       ier.created_at
FROM institution_employee_roles ier
JOIN employees e ON e.id = ier.employee_id AND e.deleted_at IS NULL
LEFT JOIN institution_roles ir ON ir.id = ier.role_id AND ir.deleted_at IS NULL
WHERE ir.id IS NULL
ORDER BY e.tenant_id, ier.employee_id;
