BEGIN;

ALTER TABLE recordings
  ADD COLUMN IF NOT EXISTS resolved_role_id BIGINT,
  ADD COLUMN IF NOT EXISTS resolved_role_code VARCHAR(128),
  ADD COLUMN IF NOT EXISTS resolved_scene_scope VARCHAR(64),
  ADD COLUMN IF NOT EXISTS resolved_pipeline_code VARCHAR(128),
  ADD COLUMN IF NOT EXISTS resolved_pipeline_version VARCHAR(64),
  ADD COLUMN IF NOT EXISTS analysis_trace_id VARCHAR(128);

CREATE INDEX IF NOT EXISTS idx_recordings_resolved_pipeline
ON recordings (resolved_pipeline_code, resolved_pipeline_version);

CREATE INDEX IF NOT EXISTS idx_recordings_analysis_trace_id
ON recordings (analysis_trace_id);

COMMIT;
