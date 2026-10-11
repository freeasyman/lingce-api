ALTER TABLE public.emr_quality_runs
  ADD COLUMN IF NOT EXISTS model_audit JSONB NOT NULL DEFAULT '{}'::jsonb;
