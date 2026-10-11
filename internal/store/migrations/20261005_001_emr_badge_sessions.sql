-- 工牌接诊会话：只保存工作台与旧 Worker 之间的关联状态，不创建 Encounter。
CREATE TABLE IF NOT EXISTS emr_badge_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    device_no VARCHAR(128) NOT NULL,
    customer_id BIGINT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stopped_at TIMESTAMPTZ,
    recording_id BIGINT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT emr_badge_sessions_status_check CHECK (TRUE)
);

CREATE INDEX IF NOT EXISTS idx_emr_badge_sessions_employee_state
    ON emr_badge_sessions (tenant_id, employee_id, updated_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uk_emr_badge_sessions_active_employee
    ON emr_badge_sessions (tenant_id, employee_id)
    WHERE recording_id IS NULL AND stopped_at IS NULL;
