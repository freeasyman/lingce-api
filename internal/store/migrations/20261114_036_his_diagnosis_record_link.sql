-- P0-1 诊断单一来源：HIS 单据显式关联 EMR 病历。
-- 唯一诊断来源是 emr_records.content_json 的 diagnosis / diagnoses；
-- his_prescriptions.diagnosis 仅保留为历史兼容快照，读取时不再作为首选真值。
BEGIN;

ALTER TABLE public.his_prescriptions
    ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE public.his_lab_requests
    ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE public.his_exam_requests
    ADD COLUMN IF NOT EXISTS record_id uuid;
ALTER TABLE public.his_treatment_orders
    ADD COLUMN IF NOT EXISTS record_id uuid;

-- 历史数据回填：同一接诊取最新一份未作废病历。回填失败（例如接诊无病历）保持 NULL，
-- 读取侧仍按 encounter_id 联查病历，并把旧列诊断作为显式标注的回退来源。
UPDATE public.his_prescriptions p
SET record_id = r.id
FROM (
    SELECT DISTINCT ON (er.encounter_id) er.tenant_id, er.encounter_id, er.id
    FROM public.emr_records er
    WHERE er.voided_at IS NULL
    ORDER BY er.encounter_id, er.updated_at DESC NULLS LAST, er.created_at DESC
) r
WHERE p.record_id IS NULL
  AND p.tenant_id = r.tenant_id
  AND p.encounter_id = r.encounter_id;

UPDATE public.his_lab_requests l
SET record_id = r.id
FROM (
    SELECT DISTINCT ON (er.encounter_id) er.tenant_id, er.encounter_id, er.id
    FROM public.emr_records er
    WHERE er.voided_at IS NULL
    ORDER BY er.encounter_id, er.updated_at DESC NULLS LAST, er.created_at DESC
) r
WHERE l.record_id IS NULL
  AND l.tenant_id = r.tenant_id
  AND l.encounter_id = r.encounter_id;

UPDATE public.his_exam_requests e
SET record_id = r.id
FROM (
    SELECT DISTINCT ON (er.encounter_id) er.tenant_id, er.encounter_id, er.id
    FROM public.emr_records er
    WHERE er.voided_at IS NULL
    ORDER BY er.encounter_id, er.updated_at DESC NULLS LAST, er.created_at DESC
) r
WHERE e.record_id IS NULL
  AND e.tenant_id = r.tenant_id
  AND e.encounter_id = r.encounter_id;

UPDATE public.his_treatment_orders t
SET record_id = r.id
FROM (
    SELECT DISTINCT ON (er.encounter_id) er.tenant_id, er.encounter_id, er.id
    FROM public.emr_records er
    WHERE er.voided_at IS NULL
    ORDER BY er.encounter_id, er.updated_at DESC NULLS LAST, er.created_at DESC
) r
WHERE t.record_id IS NULL
  AND t.tenant_id = r.tenant_id
  AND t.encounter_id = r.encounter_id;

CREATE INDEX IF NOT EXISTS idx_his_prescriptions_record_id
    ON public.his_prescriptions (tenant_id, record_id);
CREATE INDEX IF NOT EXISTS idx_his_lab_requests_record_id
    ON public.his_lab_requests (tenant_id, record_id);
CREATE INDEX IF NOT EXISTS idx_his_exam_requests_record_id
    ON public.his_exam_requests (tenant_id, record_id);
CREATE INDEX IF NOT EXISTS idx_his_treatment_orders_record_id
    ON public.his_treatment_orders (tenant_id, record_id);

COMMIT;
