ALTER TABLE public.emr_record_score_current
  DROP CONSTRAINT IF EXISTS emr_record_score_current_status_check;

ALTER TABLE public.emr_record_score_current
  ADD CONSTRAINT emr_record_score_current_status_check
  CHECK (status IN ('执行中', '已完成', '待人工复核', '执行失败'));
