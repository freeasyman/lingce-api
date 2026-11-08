-- 赢客专家访问权限：deal_expert_permission_assignments（仿 followup_permission_assignments）
CREATE TABLE IF NOT EXISTS deal_expert_permission_assignments (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    role_code VARCHAR(64) NOT NULL DEFAULT '',
    abilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by BIGINT,
    updated_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, employee_id)
);

CREATE INDEX IF NOT EXISTS idx_deal_expert_permission_assignments_tenant
    ON deal_expert_permission_assignments (tenant_id);
