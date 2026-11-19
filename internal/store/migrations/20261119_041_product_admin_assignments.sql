-- 041: 通用产品管理员指派（没有独立人员权限表的产品，目前是获客先锋/content）
-- 背景：EMR/合规/随访/客户总监/赢客五个产品各有独立权限表，门户「产品管理员」
--       直接写各自表；获客先锋复用租户范围、没有独立权限表，原先 SetProductAdmin
--       直接返回 skipped，门户无法指定管理员。
-- 本表只记录「谁是某产品的管理员」（供门户展示/撤销），不改变产品内权限：
-- 获客先锋的访问范围仍由租户范围 + employee_product_overrides 决定。
-- 有独立权限表的产品不写本表，避免两套事实源。

CREATE TABLE IF NOT EXISTS product_admin_assignments (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  product_code TEXT NOT NULL,
  employee_id BIGINT NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, product_code, employee_id)
);

CREATE INDEX IF NOT EXISTS idx_product_admin_assignments_enabled
  ON product_admin_assignments (tenant_id, product_code)
  WHERE enabled;
