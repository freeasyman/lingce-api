ALTER TABLE public.emr_records
  ADD COLUMN IF NOT EXISTS current_score_status VARCHAR(16) NOT NULL DEFAULT '未评分',
  ADD COLUMN IF NOT EXISTS current_total_score INTEGER,
  ADD COLUMN IF NOT EXISTS current_grade VARCHAR(8) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS current_score_version VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS current_quality_run_id UUID;

ALTER TABLE public.emr_record_snapshots
  ADD COLUMN IF NOT EXISTS score_status VARCHAR(16) NOT NULL DEFAULT '未评分',
  ADD COLUMN IF NOT EXISTS total_score INTEGER,
  ADD COLUMN IF NOT EXISTS grade VARCHAR(8) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS score_version VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS quality_run_id UUID;

CREATE INDEX IF NOT EXISTS emr_records_current_grade_idx ON public.emr_records (tenant_id, current_grade, updated_at DESC);
CREATE INDEX IF NOT EXISTS emr_record_snapshots_quality_run_idx ON public.emr_record_snapshots (record_id, quality_run_id);
