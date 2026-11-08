-- 介入信号：负责人需要处理的一件事（manager_signal.level=紧要 且有 sales_tasks）。
-- Web 工作台「待我处理」与 H5「待介入」共用此数据源。
CREATE TABLE IF NOT EXISTS deal_intervention_signals (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    recording_id BIGINT NOT NULL,
    analysis_version VARCHAR(32) NOT NULL DEFAULT 'v0.17',
    consultant_employee_id BIGINT NOT NULL DEFAULT 0,
    manager_employee_id BIGINT NOT NULL DEFAULT 0,
    level TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '',
    customer_name TEXT NOT NULL DEFAULT '',
    project_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    short_code TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    handled_at TIMESTAMPTZ,
    handled_by BIGINT,
    UNIQUE (tenant_id, recording_id, analysis_version)
);

CREATE INDEX IF NOT EXISTS idx_deal_intervention_signals_tenant_status
    ON deal_intervention_signals (tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deal_intervention_signals_manager
    ON deal_intervention_signals (tenant_id, manager_employee_id, status);
