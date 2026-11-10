-- 赢客专家访问权限：按人配置数据可见范围。
-- self   仅自己
-- custom 指定员工（visible_employee_ids 精确列表，可含自己）
-- tenant 全机构（仅机构管理员固定）
ALTER TABLE deal_expert_permission_assignments
    ADD COLUMN IF NOT EXISTS data_scope VARCHAR(16) NOT NULL DEFAULT 'self',
    ADD COLUMN IF NOT EXISTS visible_employee_ids BIGINT[] NOT NULL DEFAULT '{}';

ALTER TABLE deal_expert_permission_assignments
    DROP CONSTRAINT IF EXISTS deal_expert_permission_assignments_data_scope_check;

ALTER TABLE deal_expert_permission_assignments
    ADD CONSTRAINT deal_expert_permission_assignments_data_scope_check
    CHECK (data_scope IN ('self', 'custom', 'tenant'));
