CREATE TABLE IF NOT EXISTS compliance_rules (
  id TEXT PRIMARY KEY,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  code VARCHAR(128) NOT NULL,
  name VARCHAR(256) NOT NULL,
  category VARCHAR(64) NOT NULL,
  scope VARCHAR(32) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  severity VARCHAR(16) NOT NULL,
  is_built_in BOOLEAN NOT NULL DEFAULT FALSE,
  trigger_type VARCHAR(32) NOT NULL,
  conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
  description TEXT NOT NULL DEFAULT '',
  legal_basis TEXT NOT NULL DEFAULT '',
  suggested_script TEXT NOT NULL DEFAULT '',
  examples_json JSONB NOT NULL DEFAULT '{"risky":[],"safe":[]}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL,
  CONSTRAINT compliance_rules_scope_check
    CHECK (scope IN ('communication', 'content')),
  CONSTRAINT compliance_rules_severity_check
    CHECK (severity IN ('critical', 'high', 'medium', 'low')),
  CONSTRAINT compliance_rules_trigger_type_check
    CHECK (trigger_type IN ('keywords', 'semantic', 'pattern'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_compliance_rules_tenant_code
  ON compliance_rules (tenant_id, code)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_rules_tenant_scope
  ON compliance_rules (tenant_id, scope, enabled)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_rules_tenant_category
  ON compliance_rules (tenant_id, category)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_rules_tenant_builtin
  ON compliance_rules (tenant_id, is_built_in)
  WHERE deleted_at IS NULL;

INSERT INTO compliance_rules (
  id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
  trigger_type, conditions, description, legal_basis, suggested_script, examples_json
) VALUES (
  'rule-001',
  0,
  'guarantee.direct',
  '规则1.1：直接保证',
  'guarantee',
  'communication',
  TRUE,
  'critical',
  TRUE,
  'keywords',
  '{"type":"keywords","keywords":["保证","治愈","治好","康复","恢复","痊愈"],"operator":"AND"}'::jsonb,
  '直接对诊疗效果作出保证性承诺，属于第一优先级风险。',
  '《医疗广告管理办法》第七条：不得对诊疗效果作出保证性承诺。',
  '成功率较高，但因个体差异，不能保证一定达到某个结果。',
  '{"risky":["我保证你一定能治好","保证你术后视力能恢复到1.5"],"safe":["成功率在95%以上，但因个体差异，不能保证","大多数患者术后视力可以恢复到1.2-1.5"]}'::jsonb
) ON CONFLICT (id) DO NOTHING;

INSERT INTO compliance_rules (
  id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
  trigger_type, conditions, description, legal_basis, suggested_script, examples_json
) VALUES (
  'rule-002',
  0,
  'guarantee.implied',
  '规则1.2：暗示保证',
  'guarantee',
  'communication',
  TRUE,
  'critical',
  TRUE,
  'semantic',
  '{"type":"semantic","expression":"一定|肯定|绝对|必然 + 能|会 + 治好|康复|恢复"}'::jsonb,
  '使用绝对化表达，构成变相保证。',
  '《医疗广告管理办法》第七条：不得含有保证性承诺内容。',
  '有较大概率恢复，但仍需根据检查结果和个体差异判断。',
  '{"risky":["你这个一定能治好","肯定会恢复的"],"safe":["有很大概率能治好，但不能说一定","大多数情况下会恢复，但也有个体差异"]}'::jsonb
) ON CONFLICT (id) DO NOTHING;

INSERT INTO compliance_rules (
  id, tenant_id, code, name, category, scope, enabled, severity, is_built_in,
  trigger_type, conditions, description, legal_basis, suggested_script, examples_json
) VALUES (
  'rule-003',
  0,
  'fear.severity',
  '规则3.1：夸大病情',
  'fear_mongering',
  'communication',
  TRUE,
  'critical',
  TRUE,
  'keywords',
  '{"type":"keywords","keywords":["很严重","非常危险","再不做就来不及了","拖不得"],"operator":"OR"}'::jsonb,
  '通过夸大风险推动患者快速决策。',
  '医疗服务宣传和消费者保护要求，禁止利用恐惧进行误导。',
  '病情需要及时处理，但具体风险和时间判断应基于检查结果说明。',
  '{"risky":["你这个很严重，再不做就瞎了","你这个拖不得，再拖就完了"],"safe":["建议尽快手术，以免影响日常生活","需要及时治疗，但也不用过度紧张"]}'::jsonb
) ON CONFLICT (id) DO NOTHING;
