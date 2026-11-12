-- 机构管理员「主管理员」标记（幂等）。
--
-- 背景：机构管理员在仓库里的落点是 employees + institution_employee_roles(role_code=admin)，
-- 机构端登录（LoginEmployee）也只认 employees。此前「主管理员」靠
-- employees.phone = tenants.contact_phone 临时推断，无法显式更换；本迁移把主管理员变成
-- 一个显式字段，供运营端「编辑机构 / 管理员管理」使用。
--
-- 约束：
--   · 每个租户最多一个「有效主管理员」：部分唯一索引 (tenant_id) WHERE is_primary；
--   · 回填优先匹配 contact_phone 对应的启用 admin 员工；匹配不到时退化为该租户
--     id 最小的启用 admin 员工（与任务口径一致）；
--   · 全部语句幂等，可重复执行。

ALTER TABLE employees
    ADD COLUMN IF NOT EXISTS is_primary BOOLEAN NOT NULL DEFAULT FALSE;

-- 历史脏数据兜底：同一租户若已有多条主管理员，只保留 id 最小的那条。
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY tenant_id ORDER BY id) AS rn
    FROM employees
    WHERE is_primary = TRUE
      AND deleted_at IS NULL
)
UPDATE employees e
SET is_primary = FALSE,
    updated_at = NOW()
FROM ranked r
WHERE e.id = r.id
  AND r.rn > 1;

-- 回填 1：手机号等于租户联系人的启用 admin 员工。
WITH candidate AS (
    SELECT DISTINCT ON (e.tenant_id) e.id
    FROM employees e
    JOIN tenants t ON t.id = e.tenant_id
    WHERE e.deleted_at IS NULL
      AND lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
      AND trim(COALESCE(e.phone, '')) <> ''
      AND trim(e.phone) = trim(COALESCE(t.contact_phone, ''))
      AND EXISTS (
          SELECT 1
          FROM institution_employee_roles er
          LEFT JOIN institution_roles r ON r.id = er.role_id AND r.deleted_at IS NULL
          WHERE er.tenant_id = e.tenant_id
            AND er.employee_id = e.id
            AND lower(COALESCE(NULLIF(er.role_code, ''), r.code, '')) = 'admin'
      )
      AND NOT EXISTS (
          SELECT 1 FROM employees p
          WHERE p.tenant_id = e.tenant_id
            AND p.is_primary
            AND p.deleted_at IS NULL
      )
    ORDER BY e.tenant_id, e.id
)
UPDATE employees e
SET is_primary = TRUE,
    updated_at = NOW()
FROM candidate c
WHERE e.id = c.id;

-- 回填 2：没有联系人手机号匹配的租户，取 id 最小的启用 admin 员工。
WITH candidate AS (
    SELECT DISTINCT ON (e.tenant_id) e.id
    FROM employees e
    WHERE e.deleted_at IS NULL
      AND lower(COALESCE(e.is_active::text, 'true')) IN ('1', 't', 'true', 'yes')
      AND EXISTS (
          SELECT 1
          FROM institution_employee_roles er
          LEFT JOIN institution_roles r ON r.id = er.role_id AND r.deleted_at IS NULL
          WHERE er.tenant_id = e.tenant_id
            AND er.employee_id = e.id
            AND lower(COALESCE(NULLIF(er.role_code, ''), r.code, '')) = 'admin'
      )
      AND NOT EXISTS (
          SELECT 1 FROM employees p
          WHERE p.tenant_id = e.tenant_id
            AND p.is_primary
            AND p.deleted_at IS NULL
      )
    ORDER BY e.tenant_id, e.id
)
UPDATE employees e
SET is_primary = TRUE,
    updated_at = NOW()
FROM candidate c
WHERE e.id = c.id;

-- 每租户最多一个有效主管理员（软删的不算）。
CREATE UNIQUE INDEX IF NOT EXISTS uk_employees_tenant_primary
    ON employees (tenant_id)
    WHERE is_primary AND deleted_at IS NULL;

-- 管理员管理列表/主管理员查询按租户过滤，补一个覆盖索引。
CREATE INDEX IF NOT EXISTS idx_employees_tenant_primary
    ON employees (tenant_id, is_primary);
