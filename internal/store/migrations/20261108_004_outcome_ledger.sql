-- 成交业绩台账：默认入账，负责人可更正/驳回并留痕。
ALTER TABLE deal_outcome_events ADD COLUMN IF NOT EXISTS voided_at timestamptz;
ALTER TABLE deal_outcome_events ADD COLUMN IF NOT EXISTS void_reason text NOT NULL DEFAULT '';
ALTER TABLE deal_outcome_events ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE deal_outcome_events ADD COLUMN IF NOT EXISTS updated_by bigint;
ALTER TABLE deal_outcome_events ADD COLUMN IF NOT EXISTS update_reason text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_deal_outcome_events_tenant_time
  ON deal_outcome_events (tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_deal_outcome_events_tenant_recording
  ON deal_outcome_events (tenant_id, recording_id);
