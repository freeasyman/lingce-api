-- 赢客专家实时推送（干跑链路）：只记录事件与短链，不发送短信/企微。
CREATE TABLE IF NOT EXISTS deal_push_events (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    recording_id BIGINT NOT NULL,
    analysis_version VARCHAR(32) NOT NULL DEFAULT '',
    recipient_employee_id BIGINT NOT NULL,
    channel VARCHAR(32) NOT NULL DEFAULT 'sms',
    template_code VARCHAR(64) NOT NULL DEFAULT '',
    short_code VARCHAR(16) NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(32) NOT NULL DEFAULT 'dry_run',
    error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    handled_at TIMESTAMPTZ,
    UNIQUE (tenant_id, recording_id, analysis_version, recipient_employee_id)
);

CREATE INDEX IF NOT EXISTS idx_deal_push_events_tenant ON deal_push_events (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_deal_push_events_recording ON deal_push_events (tenant_id, recording_id);

CREATE TABLE IF NOT EXISTS deal_short_links (
    code VARCHAR(16) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    recording_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deal_short_links_tenant ON deal_short_links (tenant_id);
CREATE INDEX IF NOT EXISTS idx_deal_short_links_recording ON deal_short_links (recording_id);
CREATE INDEX IF NOT EXISTS idx_deal_short_links_expires ON deal_short_links (expires_at);
