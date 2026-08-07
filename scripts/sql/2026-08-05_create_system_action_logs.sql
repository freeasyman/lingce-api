CREATE TABLE IF NOT EXISTS system_action_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT,
    tenant_name TEXT,
    actor_id BIGINT,
    actor_name TEXT,
    actor_role_code TEXT,
    actor_role_name TEXT,
    log_type TEXT NOT NULL,
    action_code TEXT NOT NULL,
    action_name TEXT NOT NULL,
    route_path TEXT,
    result TEXT NOT NULL DEFAULT 'success',
    error_message TEXT,
    object_type TEXT,
    object_id TEXT,
    object_name TEXT,
    request_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    before_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip_address TEXT,
    user_agent TEXT,
    device_type TEXT,
    trace_id TEXT,
    request_id TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_system_action_logs_created_at
    ON system_action_logs(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_system_action_logs_tenant_created_at
    ON system_action_logs(tenant_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_system_action_logs_actor_created_at
    ON system_action_logs(actor_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_system_action_logs_action_created_at
    ON system_action_logs(action_code, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_system_action_logs_type_result_created_at
    ON system_action_logs(log_type, result, created_at DESC);
