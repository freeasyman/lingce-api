-- 042: 获客先锋（content）独立产品管理员权限表
-- 背景：其余五个产品（EMR/合规/随访/客户总监/赢客）都有 *_permission_assignments，
--       门户「产品管理员」按同样口径读写；获客先锋此前没有表，SetProductAdmin 直接
--       skipped，门户无法指定/撤销管理员（041 的通用指派表是过渡方案）。
-- 本迁移把获客先锋补成与其它产品一致：独立权限表 + content_admin 角色 + abilities，
-- 并撤回 041 的通用指派表，避免两套事实源。
-- 注：获客先锋的数据范围仍是租户级（无逐人 scope），本表只承载「谁是管理员」。

CREATE TABLE IF NOT EXISTS content_permission_assignments (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  employee_id BIGINT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  role_code VARCHAR(64) NOT NULL DEFAULT 'content_staff',
  abilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_content_permission_tenant_employee UNIQUE (tenant_id, employee_id),
  CONSTRAINT chk_content_permission_role_code
    CHECK (role_code IN ('content_admin', 'content_staff')),
  CONSTRAINT chk_content_permission_abilities_array
    CHECK (jsonb_typeof(abilities_json) = 'array')
);

CREATE INDEX IF NOT EXISTS idx_content_permission_tenant_id
  ON content_permission_assignments (tenant_id);

CREATE INDEX IF NOT EXISTS idx_content_permission_employee_id
  ON content_permission_assignments (employee_id);

DROP TABLE IF EXISTS product_admin_assignments;
