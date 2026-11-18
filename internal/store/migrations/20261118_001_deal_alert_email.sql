-- 赢客专家邮件提醒：租户内「谁收 / 谁抄送」路由 + 逐收件人投递记录。
-- 只新增表，不改 deal_push_events 的短信/Expo 状态；deliveries 与推送事件解耦。
CREATE TABLE IF NOT EXISTS deal_alert_routes (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    owner_employee_id BIGINT NOT NULL,
    event_type VARCHAR(16) NOT NULL CHECK (event_type IN ('consultant', 'manager')),
    to_employee_ids BIGINT[] NOT NULL DEFAULT '{}',
    cc_employee_ids BIGINT[] NOT NULL DEFAULT '{}',
    created_by BIGINT NOT NULL DEFAULT 0,
    updated_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, owner_employee_id, event_type)
);

CREATE INDEX IF NOT EXISTS idx_deal_alert_routes_tenant
    ON deal_alert_routes (tenant_id, owner_employee_id);

CREATE TABLE IF NOT EXISTS deal_email_deliveries (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    event_id BIGINT NOT NULL,
    recipient_employee_id BIGINT NOT NULL DEFAULT 0,
    email TEXT NOT NULL DEFAULT '',
    kind VARCHAR(8) NOT NULL DEFAULT 'to' CHECK (kind IN ('to', 'cc')),
    status VARCHAR(16) NOT NULL DEFAULT 'skipped' CHECK (status IN ('sent', 'failed', 'skipped')),
    message_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 幂等键：同一事件同一员工只投递一次；recipient_employee_id=0 表示事件级 skipped。
    UNIQUE (event_id, recipient_employee_id)
);

CREATE INDEX IF NOT EXISTS idx_deal_email_deliveries_tenant_event
    ON deal_email_deliveries (tenant_id, event_id);
CREATE INDEX IF NOT EXISTS idx_deal_email_deliveries_tenant_recipient
    ON deal_email_deliveries (tenant_id, recipient_employee_id, created_at DESC);
