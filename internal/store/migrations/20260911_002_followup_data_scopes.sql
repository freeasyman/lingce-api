-- 随访数据可见范围关系表。
-- 署名：Codex
-- 时间：2026-09-11
--
-- 每一行表示 viewer_employee_id 可以查看 target_employee_id 归属的数据。
-- 本表只负责随访产品的数据可见范围，不改变现有 RBAC 功能权限，
-- 也不替代 employee_partnerships 的任务执行人分配关系。

CREATE TABLE IF NOT EXISTS followup_data_scopes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    viewer_employee_id INTEGER NOT NULL,
    target_employee_id INTEGER NOT NULL,
    created_by INTEGER,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_followup_data_scopes_relation
        UNIQUE (tenant_id, viewer_employee_id, target_employee_id)
);

CREATE INDEX IF NOT EXISTS idx_followup_data_scopes_viewer
    ON followup_data_scopes (tenant_id, viewer_employee_id);

CREATE INDEX IF NOT EXISTS idx_followup_data_scopes_target
    ON followup_data_scopes (tenant_id, target_employee_id);
