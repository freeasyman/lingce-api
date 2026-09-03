CREATE TABLE IF NOT EXISTS compliance_analysis_jobs (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  recording_id BIGINT NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'queued',
  attempts INTEGER NOT NULL DEFAULT 0,
  available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  started_at TIMESTAMPTZ NULL,
  completed_at TIMESTAMPTZ NULL,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT compliance_analysis_jobs_status_check
    CHECK (status IN ('queued', 'processing', 'completed', 'failed')),
  CONSTRAINT compliance_analysis_jobs_identity_unique
    UNIQUE (tenant_id, recording_id)
);

CREATE INDEX IF NOT EXISTS idx_compliance_analysis_jobs_queue
  ON compliance_analysis_jobs (status, available_at, created_at)
  WHERE status IN ('queued', 'processing');

CREATE INDEX IF NOT EXISTS idx_compliance_analysis_jobs_tenant
  ON compliance_analysis_jobs (tenant_id, created_at DESC);
