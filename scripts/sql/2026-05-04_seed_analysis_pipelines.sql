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
  ('doctor', 'v1', '医生录音分析 v1', 'doctor', 'published', 'default doctor pipeline', TRUE, NOW(), NOW()),
  ('therapist', 'v1', '治疗师录音分析 v1', 'therapist', 'published', 'default therapist pipeline', TRUE, NOW(), NOW()),
  ('frontdesk', 'v1', '前台录音分析 v1', 'frontdesk', 'published', 'default frontdesk pipeline', TRUE, NOW(), NOW()),
  ('consultant', 'v1', '咨询录音分析 v1', 'consultant', 'published', 'default consultant pipeline', TRUE, NOW(), NOW()),
  ('lingce_sales', 'v1', '销售录音分析 v1', 'lingce_sales', 'published', 'default lingce sales pipeline', TRUE, NOW(), NOW()),
  ('customer', 'v1', '客户录音分析 v1', 'customer', 'published', 'default customer pipeline', TRUE, NOW(), NOW())
ON CONFLICT (pipeline_code, pipeline_version) DO UPDATE
SET
  name = EXCLUDED.name,
  scene_scope = EXCLUDED.scene_scope,
  status = EXCLUDED.status,
  description = EXCLUDED.description,
  is_default_candidate = EXCLUDED.is_default_candidate,
  updated_at = NOW();

COMMIT;
