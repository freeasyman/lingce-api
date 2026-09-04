-- Compliance Guard phase 2A: formalize the first three communication rules.
-- Legacy compliance_rules and existing guard runs remain immutable.

ALTER TABLE guard_rule_versions
  ADD COLUMN IF NOT EXISTS rule_card JSONB NOT NULL DEFAULT '{}'::jsonb;

-- Retire the technical-validation run so the formal rule-card version can
-- rescan the same source without presenting duplicate current findings.
UPDATE guard_analysis_runs
SET status = 'cancelled',
    failure_reason = 'superseded by formal rule-card version 20260903.2',
    updated_at = NOW()
WHERE strategy_code = 'communication.short_recording'
  AND strategy_version = '20260903.2'
  AND status = 'completed';

INSERT INTO guard_rule_versions (
  tenant_id, rule_code, rule_name, subject, source_kind, version, status,
  applicability, basis_slice, execution_spec, rule_card, created_at, published_at
) VALUES
  (
    NULL, 'communication.guarantee.direct', '诊疗效果保证性承诺', 'communication',
    'platform', '20260903.2', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现对诊疗结果作出保证性承诺的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"keyword","operator":"AND","keywords":["保证","治好"],"exclude_phrases":["不能保证","无法保证","不保证","不敢保证"]}'::jsonb,
    '{"source_basis":["legacy.communication.direct_guarantee"],"fact_definition":"同一原始转写片段同时出现保证性表达和诊疗结果表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["不能保证","无法保证","不保证","不敢保证"],"indeterminate_boundary":"仅凭片段无法判断医学依据、患者提问语境或最终责任时，只作为待复核线索。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.guarantee.absolute', '绝对化诊疗效果表述', 'communication',
    'platform', '20260903.2', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现绝对化表达与诊疗结果词同时出现的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"pattern","groups":[["一定","肯定","绝对","必然"],["能","会"],["治好","康复","恢复"]],"exclude_phrases":["不能保证一定","不一定能","不一定会","无法保证一定"]}'::jsonb,
    '{"source_basis":["legacy.communication.implied_guarantee"],"fact_definition":"同一原始转写片段同时出现绝对化表达、结果可能性表达和诊疗结果表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["不能保证一定","不一定能","不一定会","无法保证一定"],"indeterminate_boundary":"绝对化词语可能出现在否定、引用或患者提问中，缺少上下文时不得作最终定性。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.fear.exaggeration', '夸大风险推动决策', 'communication',
    'platform', '20260903.2', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现可能利用严重后果或时间压力推动患者决策的候选表述；最终是否构成违规由医院复核。',
    '{"detector":"keyword","operator":"OR","keywords":["很严重","非常危险","再不做就来不及了","拖不得"],"exclude_phrases":["不用担心","不必过度担心","没有那么严重","不严重"]}'::jsonb,
    '{"source_basis":["legacy.communication.exaggerated_severity"],"fact_definition":"原始转写片段出现可能利用严重后果或时间压力推动决策的表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["不用担心","不必过度担心","没有那么严重","不严重"],"indeterminate_boundary":"严重程度和处理时限需要结合检查、病历和医学依据；本规则只能提供候选线索。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation"}'::jsonb,
    NOW(), NOW()
  )
ON CONFLICT DO NOTHING;

INSERT INTO guard_rule_set_versions (
  tenant_id, rule_set_code, rule_set_name, subject, source_kind, version, status,
  applicability, created_at, published_at
) VALUES
  (
    NULL, 'communication.doctor.outpatient', '医生门诊诊疗沟通规则集', 'communication',
    'platform', '20260903.2', 'published',
    '{"role_codes":["doctor"],"source_types":["recording"],"rule_stage":"formal_rule_card_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.consultant.consultation', '咨询师咨询成交沟通规则集', 'communication',
    'platform', '20260903.2', 'published',
    '{"role_codes":["consultant"],"source_types":["recording"],"rule_stage":"formal_rule_card_validation"}'::jsonb,
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
 AND rule_version.version = '20260903.2'
WHERE rule_set.tenant_id IS NULL
  AND rule_set.subject = 'communication'
  AND rule_set.status = 'published'
  AND rule_set.version = '20260903.2'
  AND rule_set.rule_set_code IN (
    'communication.doctor.outpatient',
    'communication.consultant.consultation'
  )
ON CONFLICT DO NOTHING;
