-- 统一病历状态枚举：已确认改为已完成，增加待归档，移除待确认。
ALTER TABLE public.emr_records
  DROP CONSTRAINT IF EXISTS emr_records_status_check;

UPDATE public.emr_records
SET status = '已完成', updated_at = NOW()
WHERE status IN ('待确认', '已确认');

ALTER TABLE public.emr_records
  ADD CONSTRAINT emr_records_status_check
  CHECK (status IN ('草稿', '需补全', '退回', '已完成', '待归档', '已归档', '已作废'));

ALTER TABLE public.emr_process_records
  DROP CONSTRAINT IF EXISTS emr_process_status_check;

UPDATE public.emr_process_records
SET before_status = '已完成'
WHERE before_status IN ('待确认', '已确认');

UPDATE public.emr_process_records
SET after_status = '已完成'
WHERE after_status IN ('待确认', '已确认');

ALTER TABLE public.emr_process_records
  ADD CONSTRAINT emr_process_status_check
  CHECK (
    (before_status IS NULL OR before_status IN ('草稿', '需补全', '退回', '已完成', '待归档', '已归档', '已作废'))
    AND (after_status IS NULL OR after_status IN ('草稿', '需补全', '退回', '已完成', '待归档', '已归档', '已作废'))
  );
