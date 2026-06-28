BEGIN;

INSERT INTO analysis_pipelines (
  pipeline_code,
  pipeline_version,
  name,
  scene_scope,
  status,
  description,
  is_default_candidate,
  created_at,
  updated_at
)
VALUES
  ('doctor', 'v1', '医生录音分析 v1', 'post_call_analysis', 'published', 'legacy doctor pipeline', TRUE, NOW(), NOW()),
  ('doctor_patient', 'v1', '医生我与患者分析 v1', 'recording', 'published', 'default doctor patient pipeline', TRUE, NOW(), NOW()),
  ('therapist', 'v1', '治疗师录音分析 v1', 'post_call_analysis', 'published', 'default therapist pipeline', TRUE, NOW(), NOW()),
  ('frontdesk', 'v1', '前台录音分析 v1', 'frontdesk_reception', 'published', 'default frontdesk pipeline', TRUE, NOW(), NOW()),
  ('consultant', 'v1', '咨询录音分析 v1', 'admission_consult', 'published', 'legacy consultant pipeline', TRUE, NOW(), NOW()),
  ('consultant_conversion', 'v1', '咨询成交转化分析 v1', 'recording', 'published', 'default consultant conversion pipeline', TRUE, NOW(), NOW()),
  ('lingce_sales', 'v1', '销售录音分析 v1', 'admission_consult', 'published', 'default lingce sales pipeline', TRUE, NOW(), NOW()),
  ('customer', 'v1', '客户录音分析 v1', 'followup_quality', 'published', 'default customer pipeline', TRUE, NOW(), NOW())
ON CONFLICT (pipeline_code, pipeline_version) DO UPDATE
SET
  name = EXCLUDED.name,
  scene_scope = EXCLUDED.scene_scope,
  status = EXCLUDED.status,
  description = EXCLUDED.description,
  is_default_candidate = EXCLUDED.is_default_candidate,
  updated_at = NOW();

COMMIT;
