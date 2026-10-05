ALTER TABLE public.emr_quality_runs
  ADD COLUMN IF NOT EXISTS input_digest VARCHAR(128) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS emr_quality_runs_reusable_idx
  ON public.emr_quality_runs (tenant_id, record_id, action, rule_set_version, input_digest);
