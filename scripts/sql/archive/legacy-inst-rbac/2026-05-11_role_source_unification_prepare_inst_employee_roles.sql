-- 角色真源统一: 生产预备脚本
-- 时间: 2026-05-11
-- 目的:
-- 1. 补齐 inst_employee_roles 作为真源表所需的基础字段
-- 2. 补齐必要索引
-- 3. 不在本脚本中直接加“单员工唯一角色”约束
--
-- 执行顺序:
-- 1. 先执行本脚本
-- 2. 再执行 audit / backfill
-- 3. 再清理冲突与多角色
-- 4. 最后再执行唯一约束脚本

ALTER TABLE IF EXISTS inst_employee_roles
    ADD COLUMN IF NOT EXISTS source TEXT;

ALTER TABLE IF EXISTS inst_employee_roles
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP NOT NULL DEFAULT NOW();

UPDATE inst_employee_roles
SET source = COALESCE(NULLIF(source, ''), 'legacy_unknown')
WHERE source IS NULL OR source = '';

UPDATE inst_employee_roles
SET updated_at = COALESCE(updated_at, created_at, NOW())
WHERE updated_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_inst_employee_roles_tenant_employee
    ON inst_employee_roles (tenant_id, employee_id);

CREATE INDEX IF NOT EXISTS idx_inst_employee_roles_tenant_role_code
    ON inst_employee_roles (tenant_id, role_code);

CREATE INDEX IF NOT EXISTS idx_inst_employee_roles_employee_created_at
    ON inst_employee_roles (employee_id, created_at DESC);

-- 如果目标库中已经有医疗扩展字段，不做任何事；
-- 如果没有，也不在这里强行推断业务值。
ALTER TABLE IF EXISTS inst_employee_roles
    ADD COLUMN IF NOT EXISTS medical_specialty_code TEXT;

ALTER TABLE IF EXISTS inst_employee_roles
    ADD COLUMN IF NOT EXISTS specialty_group TEXT;
