-- emr_ai_generations is not used by the current EMR generation chain.
-- The active path writes generated content to emr_records and the operation
-- record to emr_process_records.

DROP TABLE IF EXISTS public.emr_ai_generations;
