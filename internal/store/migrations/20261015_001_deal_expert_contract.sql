CREATE TABLE IF NOT EXISTS deal_expert_analyses (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  recording_id BIGINT NOT NULL,
  encounter_id BIGINT NOT NULL,
  employee_id BIGINT NOT NULL,
  customer_id BIGINT,
  customer_name TEXT NOT NULL DEFAULT '',
  business_scope TEXT NOT NULL DEFAULT 'consultant',
  role_code TEXT NOT NULL DEFAULT 'consultant',
  recorded_at TIMESTAMPTZ,
  cleaned_transcript TEXT NOT NULL DEFAULT '',
  analysis_pipeline_version TEXT NOT NULL,
  prompt_version TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'received',
  result JSONB NOT NULL DEFAULT '{}'::jsonb,
  error_message TEXT NOT NULL DEFAULT '',
  received_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  analyzed_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, recording_id, analysis_pipeline_version, prompt_version)
);

CREATE INDEX IF NOT EXISTS idx_deal_expert_analyses_tenant_recorded
  ON deal_expert_analyses (tenant_id, recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_deal_expert_analyses_encounter
  ON deal_expert_analyses (tenant_id, encounter_id);
