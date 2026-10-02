-- 为体格检查栏位补齐运行时结构定义，供病历生成提示词和前端编辑器共同使用。
-- 只补齐平台默认栏位的 structure_json，不改变模板绑定关系。

BEGIN;

UPDATE public.emr_fields
SET input_type = '结构化内容',
    description = '记录生命体征、一般状态、阳性体征、必要阴性体征和专科查体。',
    structure_json = '{
      "vitals": ["temperature", "blood_pressure", "pulse", "respiration", "blood_glucose"],
      "metrics": ["height", "weight", "bmi"],
      "text_items": ["general_exam", "positive_findings", "necessary_negative_findings", "specialty_exam", "supplement"]
    }'::jsonb,
    updated_at = NOW(),
    updated_by = 0
WHERE tenant_id = 0
  AND code = 'physical_exam';

COMMIT;
