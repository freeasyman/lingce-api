-- HIS 角色扩展与「角色 × 模块」权限。
-- 员工仍是共享 employees 表；his_staff_roles 只是给员工挂角色，本迁移把角色扩为 9 个，
-- 并新增 his_role_modules 定义每个角色能进哪些模块（tenant_id=0 为默认模板，租户可覆盖）。

-- 1) 角色集合扩为 9 个（沿用目标产品的角色清单）
ALTER TABLE his_staff_roles DROP CONSTRAINT IF EXISTS his_staff_roles_role_check;
ALTER TABLE his_staff_roles ADD CONSTRAINT his_staff_roles_role_check
  CHECK (role IN ('admin','doctor','assistant','nurse','therapist','labtech','checktech','chief','other'));

-- 2) 角色 × 模块权限
CREATE TABLE IF NOT EXISTS his_role_modules (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT NOT NULL REFERENCES tenants(id),
    role       TEXT   NOT NULL,
    module     TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, role, module)
);

-- 3) 默认模块映射（tenant_id=0 模板；admin 由代码放行，不落行）
INSERT INTO his_role_modules (tenant_id, role, module) VALUES
  (0, 'doctor',    'home'),      (0, 'doctor',    'clinic'),  (0, 'doctor',    'patients'), (0, 'doctor',    'stats'),
  (0, 'assistant', 'home'),      (0, 'assistant', 'clinic'),  (0, 'assistant', 'patients'),
  (0, 'nurse',     'home'),      (0, 'nurse',     'execute'), (0, 'nurse',     'patients'),
  (0, 'therapist', 'home'),      (0, 'therapist', 'execute'), (0, 'therapist', 'patients'),
  (0, 'labtech',   'home'),      (0, 'labtech',   'lab'),
  (0, 'checktech', 'home'),      (0, 'checktech', 'check'),
  (0, 'chief',     'home'),      (0, 'chief',     'patients'), (0, 'chief',     'stats'),
  (0, 'other',     'home')
ON CONFLICT (tenant_id, role, module) DO NOTHING;
