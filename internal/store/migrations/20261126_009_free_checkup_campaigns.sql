-- 免费体检投放码：渠道 + 销售 + 场景 + 活动名 + 随机令牌（不可读 URL）
CREATE TABLE IF NOT EXISTS free_checkup_campaigns (
  id BIGSERIAL PRIMARY KEY,
  token TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL DEFAULT '',
  channel_id BIGINT NOT NULL DEFAULT 0,
  channel_name TEXT NOT NULL DEFAULT '',
  sales_id BIGINT NOT NULL DEFAULT 0,
  sales_name TEXT NOT NULL DEFAULT '',
  scene TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',   -- active / disabled
  expires_at TIMESTAMPTZ,
  remark TEXT NOT NULL DEFAULT '',
  created_by BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_free_checkup_campaigns_token ON free_checkup_campaigns (token);
CREATE INDEX IF NOT EXISTS idx_free_checkup_campaigns_channel ON free_checkup_campaigns (channel_id);

-- 访问/扫码埋点
CREATE TABLE IF NOT EXISTS free_checkup_scans (
  id BIGSERIAL PRIMARY KEY,
  campaign_id BIGINT NOT NULL DEFAULT 0,
  sid TEXT NOT NULL DEFAULT '',
  device_id TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  ip_city TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_free_checkup_scans_campaign ON free_checkup_scans (campaign_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_free_checkup_scans_sid ON free_checkup_scans (sid, campaign_id, created_at DESC);

-- 归因：使用记录与线索带上投放码/渠道/销售/场景
ALTER TABLE free_checkup_usage ADD COLUMN IF NOT EXISTS campaign_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS campaign_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS channel_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS channel_name TEXT NOT NULL DEFAULT '';
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS sales_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS sales_name TEXT NOT NULL DEFAULT '';
