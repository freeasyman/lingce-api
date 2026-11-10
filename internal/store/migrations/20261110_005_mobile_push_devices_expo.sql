-- Expo Push 设备 token 与发送结果。
-- mobile_push_devices 已由移动端历史链路创建；这里补齐后端推送所需的字段与唯一约束，
-- 并在全新数据库上兜底建表（字段与历史表保持一致）。
CREATE TABLE IF NOT EXISTS mobile_push_devices (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    platform VARCHAR(20) NOT NULL DEFAULT '',
    device_id VARCHAR(120) NOT NULL DEFAULT '',
    expo_push_token VARCHAR(255) NOT NULL DEFAULT '',
    app_version VARCHAR(64),
    device_name VARCHAR(120) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_error TEXT NOT NULL DEFAULT '',
    last_sent_at TIMESTAMPTZ,
    invalidated_at TIMESTAMPTZ,
    last_seen_at TIMESTAMP NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP
);

ALTER TABLE mobile_push_devices
    ADD COLUMN IF NOT EXISTS device_name VARCHAR(120) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_sent_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS invalidated_at TIMESTAMPTZ;

-- Expo push token 全局唯一：同一 token 重新注册时做幂等 upsert。
CREATE UNIQUE INDEX IF NOT EXISTS mobile_push_devices_expo_push_token_key
    ON mobile_push_devices (expo_push_token);
CREATE UNIQUE INDEX IF NOT EXISTS uk_push_device_user_device
    ON mobile_push_devices (user_id, device_id);
CREATE INDEX IF NOT EXISTS idx_push_device_tenant_user_active
    ON mobile_push_devices (tenant_id, user_id, is_active);
CREATE INDEX IF NOT EXISTS ix_mobile_push_devices_tenant_id
    ON mobile_push_devices (tenant_id);
CREATE INDEX IF NOT EXISTS ix_mobile_push_devices_user_id
    ON mobile_push_devices (user_id);

-- 推送事件记录 Expo 发送结果，与短信 status/provider_biz_id 分开，互不覆盖。
ALTER TABLE deal_push_events
    ADD COLUMN IF NOT EXISTS expo_status VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS expo_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS expo_sent_at TIMESTAMPTZ;
