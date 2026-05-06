BEGIN;

ALTER TABLE recording_analysis_results
  ADD COLUMN IF NOT EXISTS run_id BIGINT,
  ADD COLUMN IF NOT EXISTS step_run_id BIGINT,
  ADD COLUMN IF NOT EXISTS pipeline_code VARCHAR(128),
  ADD COLUMN IF NOT EXISTS pipeline_version VARCHAR(64),
  ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_recording_analysis_results_run_id
ON recording_analysis_results (run_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_recording_analysis_results_active
ON recording_analysis_results (recording_id, is_active, created_at DESC);

COMMIT;
