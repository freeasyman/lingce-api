-- 免费体检：签名 Cookie 会话标识（每日额度主身份）
ALTER TABLE free_checkup_usage ADD COLUMN IF NOT EXISTS sid TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_free_checkup_usage_sid ON free_checkup_usage (sid, created_at DESC);
