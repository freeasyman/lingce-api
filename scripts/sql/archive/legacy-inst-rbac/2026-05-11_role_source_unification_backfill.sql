-- 角色真源统一回填脚本
-- 时间: 2026-05-11
-- 目的:
-- 1. 只回填 inst_employee_roles 缺失、但 institution_employee_roles 当前有值的员工
-- 2. 不自动覆盖双表冲突
-- 3. 幂等执行
--
-- 使用方式:
-- 1. 先执行“预览将回填哪些员工”
-- 2. 确认结果后再执行 INSERT
-- 3. 执行后重新跑审计脚本

-- 1. 预览将被回填的员工
WITH legacy_current AS (
    SELECT DISTINCT ON (er.tenant_id, er.employee_id)
           er.tenant_id,
           er.employee_id,
           lower(trim(er.role_code)) AS role_code,
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
       n.role_code,
       n.created_at AS source_created_at
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

-- 2. 回填缺失员工角色
WITH legacy_current AS (
    SELECT DISTINCT ON (er.tenant_id, er.employee_id)
           er.tenant_id,
           er.employee_id,
           lower(trim(er.role_code)) AS role_code,
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
           COALESCE(ier.created_at, NOW()) AS created_at
    FROM institution_employee_roles ier
    JOIN employees e ON e.id = ier.employee_id AND e.deleted_at IS NULL
    JOIN institution_roles ir ON ir.id = ier.role_id AND ir.deleted_at IS NULL
    ORDER BY e.tenant_id, ier.employee_id, ier.created_at DESC, ir.id DESC
),
to_backfill AS (
    SELECT n.tenant_id,
           n.employee_id,
           n.role_code,
           n.created_at
    FROM new_current n
    LEFT JOIN legacy_current l
      ON l.tenant_id = n.tenant_id
     AND l.employee_id = n.employee_id
    WHERE l.employee_id IS NULL
)
INSERT INTO inst_employee_roles (
    tenant_id,
    employee_id,
    role_code,
    source,
    created_at
)
SELECT tenant_id,
       employee_id,
       role_code,
       'backfill_from_institution_employee_roles',
       created_at
FROM to_backfill;

-- 3. 执行后建议立即复查
-- 重新执行:
-- scripts/sql/2026-05-11_role_source_unification_audit.sql
