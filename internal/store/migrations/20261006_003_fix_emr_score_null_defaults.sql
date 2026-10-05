-- 未形成正式评分结果的记录不得拥有总分或等级。
ALTER TABLE public.emr_record_score_current
  ALTER COLUMN total_score DROP NOT NULL,
  ALTER COLUMN grade DROP NOT NULL,
  ALTER COLUMN total_score DROP DEFAULT,
  ALTER COLUMN grade DROP DEFAULT;

ALTER TABLE public.emr_records
  ALTER COLUMN current_grade DROP NOT NULL,
  ALTER COLUMN current_grade DROP DEFAULT;

ALTER TABLE public.emr_record_snapshots
  ALTER COLUMN grade DROP NOT NULL,
  ALTER COLUMN grade DROP DEFAULT;

UPDATE public.emr_record_score_current
SET total_score = NULL,
    grade = NULL
WHERE status NOT IN ('已完成', '待人工复核')
   OR total_score IS NULL;

UPDATE public.emr_records
SET current_total_score = NULL,
    current_grade = NULL
WHERE current_score_status NOT IN ('已完成', '待人工复核')
   OR current_total_score IS NULL;

UPDATE public.emr_record_snapshots
SET total_score = NULL,
    grade = NULL
WHERE score_status NOT IN ('已完成', '待人工复核')
   OR total_score IS NULL;
