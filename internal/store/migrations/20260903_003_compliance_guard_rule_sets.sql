-- Compliance Guard phase 2A: versioned scene rule sets.
-- A rule remains an atomic platform rule. A rule set selects which published
-- rule versions apply to one subject/role/scene without changing legacy rules.

CREATE TABLE IF NOT EXISTS guard_rule_set_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id BIGINT,
  rule_set_code VARCHAR(128) NOT NULL,
  rule_set_name VARCHAR(255) NOT NULL DEFAULT '',
  subject VARCHAR(32) NOT NULL,
  source_kind VARCHAR(32) NOT NULL DEFAULT 'platform',
  version VARCHAR(64) NOT NULL,
  status VARCHAR(24) NOT NULL DEFAULT 'draft',
  applicability JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  published_at TIMESTAMPTZ,
  CONSTRAINT guard_rule_set_versions_tenant_check CHECK (tenant_id IS NULL OR tenant_id > 0),
  CONSTRAINT guard_rule_set_versions_subject_check CHECK (subject IN ('communication', 'medical_record', 'content')),
  CONSTRAINT guard_rule_set_versions_source_check CHECK (source_kind IN ('platform', 'local')),
  CONSTRAINT guard_rule_set_versions_status_check CHECK (status IN ('draft', 'published', 'disabled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS guard_rule_set_versions_identity_uq
  ON guard_rule_set_versions (COALESCE(tenant_id, 0), rule_set_code, version);
CREATE INDEX IF NOT EXISTS guard_rule_set_versions_lookup_idx
  ON guard_rule_set_versions (tenant_id, subject, rule_set_code, status, version DESC);

CREATE TABLE IF NOT EXISTS guard_rule_set_rule_versions (
  rule_set_version_id UUID NOT NULL REFERENCES guard_rule_set_versions(id),
  rule_version_id UUID NOT NULL REFERENCES guard_rule_versions(id),
  position INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (rule_set_version_id, rule_version_id)
);
CREATE INDEX IF NOT EXISTS guard_rule_set_rule_versions_rule_idx
  ON guard_rule_set_rule_versions (rule_version_id);

-- Keep the original 20260902.1 rules immutable for historical runs. Publish
-- the same narrow technical-validation rules again with explicit profiles.
INSERT INTO guard_rule_versions (
  tenant_id, rule_code, rule_name, subject, source_kind, version, status,
  applicability, basis_slice, execution_spec, created_at, published_at
) VALUES
  (
    NULL, 'communication.guarantee.direct', '诊疗效果保证性承诺', 'communication',
    'platform', '20260903.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现对诊疗结果作出保证性承诺的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"keyword","operator":"AND","keywords":["保证","治好"]}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.guarantee.absolute', '绝对化诊疗效果表述', 'communication',
    'platform', '20260903.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现绝对化表达与诊疗结果词同时出现的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"pattern","groups":[["一定","肯定","绝对","必然"],["能","会"],["治好","康复","恢复"]]}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.fear.exaggeration', '夸大风险推动决策', 'communication',
    'platform', '20260903.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现可能利用严重后果或时间压力推动患者决策的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"keyword","operator":"OR","keywords":["很严重","非常危险","再不做就来不及了","拖不得"]}'::jsonb,
    NOW(), NOW()
  )
ON CONFLICT DO NOTHING;

INSERT INTO guard_rule_set_versions (
  tenant_id, rule_set_code, rule_set_name, subject, source_kind, version, status,
  applicability, created_at, published_at
) VALUES
  (
    NULL, 'communication.doctor.outpatient', '医生门诊诊疗沟通规则集', 'communication',
    'platform', '20260903.1', 'published',
    '{"role_codes":["doctor"],"source_types":["recording"]}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.consultant.consultation', '咨询师咨询成交沟通规则集', 'communication',
    'platform', '20260903.1', 'published',
    '{"role_codes":["consultant"],"source_types":["recording"]}'::jsonb,
    NOW(), NOW()
  )
ON CONFLICT DO NOTHING;

INSERT INTO guard_rule_set_rule_versions (rule_set_version_id, rule_version_id, position)
SELECT rule_set.id, rule_version.id,
       CASE rule_version.rule_code
         WHEN 'communication.guarantee.direct' THEN 10
         WHEN 'communication.guarantee.absolute' THEN 20
         WHEN 'communication.fear.exaggeration' THEN 30
         ELSE 100
       END
FROM guard_rule_set_versions rule_set
JOIN guard_rule_versions rule_version
  ON rule_version.tenant_id IS NULL
 AND rule_version.subject = 'communication'
 AND rule_version.status = 'published'
 AND rule_version.version = '20260903.1'
WHERE rule_set.tenant_id IS NULL
  AND rule_set.subject = 'communication'
  AND rule_set.status = 'published'
  AND rule_set.version = '20260903.1'
  AND rule_set.rule_set_code IN (
    'communication.doctor.outpatient',
    'communication.consultant.consultation'
  )
  AND rule_version.rule_code IN (
    'communication.guarantee.direct',
    'communication.guarantee.absolute',
    'communication.fear.exaggeration'
  )
ON CONFLICT DO NOTHING;
