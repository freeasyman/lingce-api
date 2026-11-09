-- 推送事件记录阿里云短信回执：provider_biz_id（BizId）。
ALTER TABLE deal_push_events ADD COLUMN IF NOT EXISTS provider_biz_id TEXT NOT NULL DEFAULT '';
