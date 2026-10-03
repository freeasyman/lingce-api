-- Retire execution configuration; preserve standards and historical scores.
UPDATE public.recording_analysis_prompts
SET is_active = FALSE
WHERE code = 'emr_score_evaluation_v1';

UPDATE public.llm_model_configs
SET is_active = FALSE
WHERE function_type = 'emr_score_evaluation';
