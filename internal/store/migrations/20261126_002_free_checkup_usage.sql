-- 免费体检使用记录：无论是否留手机号都记一条（成本核算/使用量统计）。
CREATE TABLE IF NOT EXISTS free_checkup_usage (
  id BIGSERIAL PRIMARY KEY,
  run_id BIGINT NOT NULL DEFAULT 0,
  device_id TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  user_agent TEXT NOT NULL DEFAULT '',
  scene TEXT NOT NULL DEFAULT '',
  org_name TEXT NOT NULL DEFAULT '',
  region TEXT NOT NULL DEFAULT '',
  question TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_free_checkup_usage_created ON free_checkup_usage (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_free_checkup_usage_device ON free_checkup_usage (device_id);

-- 线索表补设备信息
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS device_id TEXT NOT NULL DEFAULT '';
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS ip TEXT NOT NULL DEFAULT '';
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS user_agent TEXT NOT NULL DEFAULT '';
