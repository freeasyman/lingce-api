CREATE TABLE IF NOT EXISTS followup_task_audits (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  task_id BIGINT NOT NULL,
  action VARCHAR(64) NOT NULL,
  detail JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_followup_task_audits_task ON followup_task_audits(tenant_id, task_id, created_at DESC);
