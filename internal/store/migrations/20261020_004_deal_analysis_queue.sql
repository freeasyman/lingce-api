-- 赢客专家分析：模型队列字段。
-- 说明：接收接口只把任务落库为 received，分析由 API 内后台消费者领取；
-- attempts/last_attempt_at 支持可重试的失败处理，避免一次失败就永远卡住。
ALTER TABLE deal_expert_analyses
  ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS model_code TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS latency_ms INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS total_tokens INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_deal_expert_analyses_status
  ON deal_expert_analyses (status, id);
