-- Compliance Guard phase 2A: rewrite every legacy rule into the new catalog.
-- This migration never reads from or changes legacy compliance_rules. Each
-- legacy source is explicitly represented as a new immutable rule-card version.

-- The new catalog will rescan the same source material. Retire the preceding
-- three-rule catalog so old and new active findings cannot be mixed.
UPDATE guard_analysis_runs
SET status = 'cancelled',
    failure_reason = 'superseded by legacy-rule catalog version 20260904.1',
    updated_at = NOW()
WHERE strategy_code = 'communication.short_recording'
  AND strategy_version = '20260903.3'
  AND status = 'completed';

INSERT INTO guard_rule_versions (
  tenant_id, rule_code, rule_name, subject, source_kind, version, status,
  applicability, basis_slice, execution_spec, rule_card, created_at, published_at
) VALUES
  (
    NULL, 'communication.guarantee.direct', '诊疗效果保证性承诺', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现明确保证诊疗结果的候选表述；系统仅提供原始证据和依据切片，最终是否成立由医院复核。',
    '{"detector":"pattern","groups":[["保证"],["治愈","治好","康复","恢复","痊愈","不会掉"]],"exclude_phrases":["不能保证","无法保证","不保证","不敢保证","不能承诺"]}'::jsonb,
    '{"source_basis":["legacy.rule-001.guarantee.direct"],"disposition":"published_short_recording_candidate","fact_definition":"同一原始转写片段同时出现保证性表达和诊疗结果表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["不能保证","无法保证","不保证","不敢保证","不能承诺"],"indeterminate_boundary":"片段不足以判断患者提问语境、医学依据或最终责任，只能作为待复核线索。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.guarantee.absolute', '绝对化诊疗效果表述', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现绝对化表达与诊疗结果词同时出现的候选表述；最终是否成立由医院复核。',
    '{"detector":"pattern","groups":[["一定","肯定","绝对","必然"],["能","会"],["治愈","治好","康复","恢复","痊愈"]],"exclude_phrases":["不能保证一定","不一定能","不一定会","无法保证一定","未必能","不敢肯定"]}'::jsonb,
    '{"source_basis":["legacy.rule-002.guarantee.implied"],"disposition":"published_short_recording_candidate","fact_definition":"同一原始转写片段同时出现绝对化表达、结果可能性表达和诊疗结果表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["不能保证一定","不一定能","不一定会","无法保证一定","未必能","不敢肯定"],"indeterminate_boundary":"绝对化词可能出现在否定、引用或患者提问中，不能据此自动定性。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.guarantee.case', '以案例暗示当前患者结果', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现引用成功案例并将结果指向当前患者的候选表述；医院需结合完整上下文复核。',
    '{"detector":"pattern","groups":[["案例","患者","做过","做完"],["你也会","你也能","你也一样","和她一样","没一个失败","全部成功","百分之百成功"]],"exclude_phrases":["每个人情况不同","因人而异","不能保证","不代表","不一定"]}'::jsonb,
    '{"source_basis":["legacy.rule-003.guarantee.case"],"disposition":"published_short_recording_candidate","fact_definition":"片段同时包含成功案例或既往结果的引用，以及对当前患者结果的一致性暗示或绝对成功表述。","evidence_requirements":["原始转写片段","说话人","起止时间","可取得时的上下文片段"],"exclusion_conditions":["每个人情况不同","因人而异","不能保证","不代表","不一定"],"indeterminate_boundary":"引用案例本身不等于违规；无法判断案例真实性、患者主动提问或个体差异说明时，只作候选。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.guarantee.duration', '无条件承诺疗效维持时间', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现把疗效或器械结果无条件承诺为长期、终身或永久的候选表述；最终是否成立由医院复核。',
    '{"detector":"pattern","groups":[["一辈子","终身","永久","20年","几十年"],["能用","有效","不会掉","不用换","一直用","维持"]],"exclude_phrases":["护理得当","理论上","可能","因人而异","需要复查","不保证"]}'::jsonb,
    '{"source_basis":["legacy.rule-004.guarantee.duration"],"disposition":"published_short_recording_candidate","fact_definition":"同一片段同时出现长期时长表达和无条件效果、使用或维持结果表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["护理得当","理论上","可能","因人而异","需要复查","不保证"],"indeterminate_boundary":"客观经验、条件性说明或器械理论寿命不当然构成承诺，必须由医院复核。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.misleading.exclusivity', '绝对化疗效、设备或方案排他宣传', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.consultant.consultation"]}'::jsonb,
    '发现把具体诊疗方案、技术、设备或机构表述为唯一、最佳或独家的候选表达；当前只适用于咨询师场景。',
    '{"detector":"pattern","groups":[["最好","最佳","唯一","独家","全国唯一"],["方案","治疗","方法","技术","设备","医院","机构"]],"exclude_phrases":["之一","各有优势","未必","不一定"]}'::jsonb,
    '{"source_basis":["legacy.rule-005.misleading.exaggerate"],"disposition":"published_consultant_only_candidate","fact_definition":"同一片段同时出现绝对化、排他性表达和被推广的诊疗方案、技术、设备或机构对象。","evidence_requirements":["原始转写片段","说话人","起止时间","可取得时的宣传或设备事实"],"exclusion_conditions":["之一","各有优势","未必","不一定"],"indeterminate_boundary":"医生对方案的医学比较需要病情依据，当前不进入医生规则集；咨询师候选也不自动确认虚假宣传。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.misleading.disparage', '无依据贬低其他机构或方案', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.consultant.consultation"]}'::jsonb,
    '发现将其他机构、医生或方案与明确贬损表达组合的候选表述；合并旧“贬低其他方案”和“诋毁同行”。',
    '{"detector":"pattern","groups":[["别的医院","其他医院","那家医院","别的方案","其他方案","传统方法","别的医生","其他医生"],["不行","骗人","太差","没用","落后","都是假的"]],"exclude_phrases":["各有优势","不代表","不能一概而论","要看情况"]}'::jsonb,
    '{"source_basis":["legacy.rule-006.misleading.degrade","legacy.rule-017.other.degrade"],"disposition":"published_consultant_only_candidate","fact_definition":"片段同时出现可识别的其他机构、医生或方案对象，以及无依据的贬损表达。","evidence_requirements":["原始转写片段","说话人","起止时间","可取得时的比较依据"],"exclusion_conditions":["各有优势","不代表","不能一概而论","要看情况"],"indeterminate_boundary":"医生可能基于病情作医学比较，当前不进入医生规则集；咨询师候选也需人工判断是否有事实依据。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.misleading.urgency', '非医学依据的成交紧迫施压', 'communication',
    'platform', '20260904.1', 'draft',
    '{"scene_codes":["communication.consultant.consultation"]}'::jsonb,
    '需要核验活动、名额、预约或价格事实后，才能判断是否构成不当成交紧迫施压。',
    '{"detector":"cross_source","required_facts":["活动规则","名额或预约记录","价格或有效期"]}'::jsonb,
    '{"source_basis":["legacy.rule-007.misleading.urgency"],"disposition":"draft_requires_external_facts","fact_definition":"录音中的时间、名额、涨价或排队表达与实际活动、价格或预约事实不一致。","evidence_requirements":["原始转写片段","活动规则","名额或预约记录","价格或有效期"],"exclusion_conditions":["真实活动且信息完整","真实预约紧张且可核验"],"missing_facts":["活动规则","名额或预约记录","价格或有效期"],"indeterminate_boundary":"录音单独不能证明紧迫表达不真实或不当。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'communication.fear.exaggeration', '夸大风险推动决策', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现可能利用严重后果或时间压力推动患者决策的候选表述；严重程度仍需医院结合医学事实复核。',
    '{"detector":"keyword","operator":"OR","keywords":["很严重","非常危险","再不做就来不及了","拖不得"],"exclude_phrases":["不用担心","不必过度担心","没有那么严重","不严重"]}'::jsonb,
    '{"source_basis":["legacy.rule-009.fear.severity"],"disposition":"published_short_recording_candidate","fact_definition":"原始转写片段出现可能利用严重后果或时间压力推动决策的表达。","evidence_requirements":["原始转写片段","说话人","起止时间"],"exclusion_conditions":["不用担心","不必过度担心","没有那么严重","不严重"],"indeterminate_boundary":"严重程度和处理时限需要结合检查、病历和医学依据；本规则只能提供候选线索。","output_type":"machine_finding","review_priority":"review","validation_status":"technical_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.fear.panic', '不做即发生绝对严重后果', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现把不做某项治疗直接关联失明、全掉光等严重后果的候选表述；医学风险是否真实必须由医院复核。',
    '{"detector":"pattern","groups":[["不做","不治疗","不手术","不种牙","不矫正"],["失明","全掉光","瞎了","恶化","严重后果"]],"exclude_phrases":["有可能","定期检查","因人而异","医生评估","需要检查"]}'::jsonb,
    '{"source_basis":["legacy.rule-010.fear.panic"],"disposition":"published_short_recording_candidate","fact_definition":"同一片段把不做治疗或矫正与绝对严重后果直接关联。","evidence_requirements":["原始转写片段","说话人","起止时间","医生场景可取得时的病历或检查事实"],"exclusion_conditions":["有可能","定期检查","因人而异","医生评估","需要检查"],"indeterminate_boundary":"医学风险说明可能合理，尤其医生场景应结合病历和检查事实复核，系统不确认夸大病情。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.fear.compare', '做与不做的夸张后果对比', 'communication',
    'platform', '20260904.1', 'published',
    '{"scene_codes":["communication.doctor.outpatient","communication.consultant.consultation"]}'::jsonb,
    '发现把治疗后绝对正面结果与不治疗后绝对负面结果并列的候选表述；最终是否不当由医院复核。',
    '{"detector":"pattern","groups":[["做了","做完","治疗后","手术后"],["不做","不治疗","不手术"],["看得清","治好","恢复","改善"],["一辈子","越来越深","失明","全掉光"]],"exclude_phrases":["可以选择","也可以","个人选择","因人而异"]}'::jsonb,
    '{"source_basis":["legacy.rule-011.fear.compare"],"disposition":"published_short_recording_candidate","fact_definition":"同一片段同时对治疗后和不治疗后的结果作绝对化正负对比。","evidence_requirements":["原始转写片段","说话人","起止时间","医生场景可取得时的病历或检查事实"],"exclusion_conditions":["可以选择","也可以","个人选择","因人而异"],"indeterminate_boundary":"治疗方案比较可能是必要告知，医生场景尤其需要医学事实复核。","output_type":"machine_finding","review_priority":"review","validation_status":"test_material_required"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.price.on_site_increase', '电话低价与到店报价不一致', 'communication',
    'platform', '20260904.1', 'draft',
    '{"scene_codes":["communication.consultant.consultation"]}'::jsonb,
    '需要比较至少两次报价、项目价格和到院记录，录音单独不能确认到店加价。',
    '{"detector":"cross_source","required_facts":["首次报价","到院报价","项目价格表","到院记录"]}'::jsonb,
    '{"source_basis":["legacy.rule-014.price.on_site"],"disposition":"draft_requires_external_facts","fact_definition":"首次沟通报价与到院后的相同项目报价不一致，且不存在可核验的材料、项目或服务范围差异。","evidence_requirements":["首次报价录音或记录","到院报价记录","项目价格表","到院记录"],"exclusion_conditions":["项目或材料不同且已告知","价格区间已完整说明"],"missing_facts":["首次报价","到院报价","项目价格表","到院记录"],"indeterminate_boundary":"单段录音不能证明实际成交价格或费用构成。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'communication.scope.cross_department', '表达服务范围与执业资质不一致', 'communication',
    'platform', '20260904.1', 'draft',
    '{"scene_codes":["communication.doctor.outpatient"]}'::jsonb,
    '需要核验人员执业范围、排班、科室和实际服务信息，录音只能提供候选表达。',
    '{"detector":"cross_source","required_facts":["执业范围","人员排班或科室","实际服务项目"]}'::jsonb,
    '{"source_basis":["legacy.rule-015.scope.cross_department"],"disposition":"draft_requires_external_facts","fact_definition":"人员在沟通中表达的服务能力与其可核验的执业范围、科室或授权不一致。","evidence_requirements":["原始转写片段","执业范围","人员排班或科室","实际服务项目"],"exclusion_conditions":["已转诊或明确说明不在本人范围","有有效授权和排班依据"],"missing_facts":["执业范围","人员排班或科室","实际服务项目"],"indeterminate_boundary":"录音不能自行证明超范围执业。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'communication.scope.untrained_technology', '表达使用新技术与培训授权不一致', 'communication',
    'platform', '20260904.1', 'draft',
    '{"scene_codes":["communication.doctor.outpatient"]}'::jsonb,
    '需要核验新技术、设备的培训与授权资料，录音单独不能确认未经培训。',
    '{"detector":"cross_source","required_facts":["培训记录","设备或技术授权","人员资格"]}'::jsonb,
    '{"source_basis":["legacy.rule-016.scope.untrained_tech"],"disposition":"draft_requires_external_facts","fact_definition":"人员表达可使用新技术或设备，但培训、授权或资格资料不能支持该表达。","evidence_requirements":["原始转写片段","培训记录","设备或技术授权","人员资格"],"exclusion_conditions":["有效培训和授权齐备","由具备资格人员实际服务"],"missing_facts":["培训记录","设备或技术授权","人员资格"],"indeterminate_boundary":"录音不能自行证明未经培训或无授权。","output_type":"cross_source_candidate","review_priority":"review","validation_status":"blocked_by_missing_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'content.price.false_original', '虚构原价或虚假优惠', 'content',
    'platform', '20260904.1', 'draft',
    '{"source_types":["content_item"]}'::jsonb,
    '内容中的原价、市场价和优惠需要与医院价格及活动资料核验。',
    '{"detector":"content_cross_source","required_facts":["价格表","活动规则","价格生效记录"]}'::jsonb,
    '{"source_basis":["legacy.rule-012.price.fake_original"],"disposition":"draft_content_pre_review","fact_definition":"内容宣称的原价、市场价或优惠与可核验的价格和活动资料不一致。","evidence_requirements":["内容版本快照","价格表","活动规则","价格生效记录"],"missing_facts":["内容版本快照","价格表","活动规则"],"indeterminate_boundary":"文字中的原价和优惠不能单独证明虚假价格。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'content.price.hidden_fee', '隐瞒费用构成', 'content',
    'platform', '20260904.1', 'draft',
    '{"source_types":["content_item"]}'::jsonb,
    '内容报价是否完整需要项目费用构成和适用条件资料。',
    '{"detector":"content_cross_source","required_facts":["项目费用构成","价格表","适用条件"]}'::jsonb,
    '{"source_basis":["legacy.rule-013.price.hidden_fee"],"disposition":"draft_content_pre_review","fact_definition":"内容中的项目报价遗漏应披露且会影响总价的费用构成。","evidence_requirements":["内容版本快照","项目费用构成","价格表","适用条件"],"missing_facts":["内容版本快照","项目费用构成","价格表"],"indeterminate_boundary":"单一价格文案不能自动证明隐瞒费用。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'content.data.unverified', '无法核验的宣传数据', 'content',
    'platform', '20260904.1', 'draft',
    '{"source_types":["content_item"]}'::jsonb,
    '内容中的满意度、无事故、万例等数据需要来源和统计口径核验。',
    '{"detector":"content_cross_source","required_facts":["数据来源","统计口径","有效期"]}'::jsonb,
    '{"source_basis":["legacy.rule-008.misleading.data"],"disposition":"draft_content_pre_review","fact_definition":"内容中使用的统计或效果数据缺少可核验来源、口径或有效期。","evidence_requirements":["内容版本快照","数据来源","统计口径","有效期"],"missing_facts":["内容版本快照","数据来源","统计口径"],"indeterminate_boundary":"内容出现数字本身不等于虚假数据。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'content.privacy.patient', '患者隐私或案例展示未获授权', 'content',
    'platform', '20260904.1', 'draft',
    '{"source_types":["content_item"]}'::jsonb,
    '患者身份、照片或案例展示需要患者授权和脱敏范围资料。',
    '{"detector":"content_cross_source","required_facts":["患者授权","脱敏范围","传播渠道"]}'::jsonb,
    '{"source_basis":["legacy.rule-018.other.privacy"],"disposition":"draft_content_pre_review","fact_definition":"内容展示可识别的患者身份、影像或案例信息，但无法提供匹配的授权和脱敏依据。","evidence_requirements":["内容版本快照","患者授权","脱敏范围","传播渠道"],"missing_facts":["内容版本快照","患者授权","脱敏范围"],"indeterminate_boundary":"内容出现案例或照片不自动证明未经授权。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source"}'::jsonb,
    NOW(), NULL
  ),
  (
    NULL, 'content.honor.unverified', '无法核验的医院或人员荣誉资质宣传', 'content',
    'platform', '20260904.1', 'draft',
    '{"source_types":["content_item"]}'::jsonb,
    '医院等级、人员资质和荣誉称号需要权威来源或医院授权资料核验。',
    '{"detector":"content_cross_source","required_facts":["资质或荣誉证明","医院授权","有效期"]}'::jsonb,
    '{"source_basis":["legacy.rule-019.other.honor_fake"],"disposition":"draft_content_pre_review","fact_definition":"内容宣称的医院等级、人员资质或荣誉无法由有效证明材料支持。","evidence_requirements":["内容版本快照","资质或荣誉证明","医院授权","有效期"],"missing_facts":["内容版本快照","资质或荣誉证明","有效期"],"indeterminate_boundary":"内容出现资质或荣誉称号不自动证明虚构。","output_type":"content_cross_source_candidate","review_priority":"review","validation_status":"blocked_by_content_source"}'::jsonb,
    NOW(), NULL
  )
ON CONFLICT DO NOTHING;

INSERT INTO guard_rule_set_versions (
  tenant_id, rule_set_code, rule_set_name, subject, source_kind, version, status,
  applicability, created_at, published_at
) VALUES
  (
    NULL, 'communication.doctor.outpatient', '医生门诊诊疗沟通规则集', 'communication',
    'platform', '20260904.1', 'published',
    '{"role_codes":["doctor"],"source_types":["recording"],"rule_stage":"legacy_catalog_technical_validation"}'::jsonb,
    NOW(), NOW()
  ),
  (
    NULL, 'communication.consultant.consultation', '咨询师咨询成交沟通规则集', 'communication',
    'platform', '20260904.1', 'published',
    '{"role_codes":["consultant"],"source_types":["recording"],"rule_stage":"legacy_catalog_technical_validation"}'::jsonb,
    NOW(), NOW()
  )
ON CONFLICT DO NOTHING;

INSERT INTO guard_rule_set_rule_versions (rule_set_version_id, rule_version_id, position)
SELECT rule_set.id, rule_version.id,
       CASE rule_version.rule_code
         WHEN 'communication.guarantee.direct' THEN 10
         WHEN 'communication.guarantee.absolute' THEN 20
         WHEN 'communication.guarantee.case' THEN 30
         WHEN 'communication.guarantee.duration' THEN 40
         WHEN 'communication.misleading.exclusivity' THEN 50
         WHEN 'communication.misleading.disparage' THEN 60
         WHEN 'communication.fear.exaggeration' THEN 70
         WHEN 'communication.fear.panic' THEN 80
         WHEN 'communication.fear.compare' THEN 90
         ELSE 100
       END
FROM guard_rule_set_versions rule_set
JOIN guard_rule_versions rule_version
  ON rule_version.tenant_id IS NULL
 AND rule_version.subject = 'communication'
 AND rule_version.status = 'published'
 AND rule_version.version = '20260904.1'
WHERE rule_set.tenant_id IS NULL
  AND rule_set.subject = 'communication'
  AND rule_set.status = 'published'
  AND rule_set.version = '20260904.1'
  AND (
    (rule_set.rule_set_code = 'communication.doctor.outpatient' AND rule_version.rule_code IN (
      'communication.guarantee.direct',
      'communication.guarantee.absolute',
      'communication.guarantee.case',
      'communication.guarantee.duration',
      'communication.fear.exaggeration',
      'communication.fear.panic',
      'communication.fear.compare'
    ))
    OR
    (rule_set.rule_set_code = 'communication.consultant.consultation' AND rule_version.rule_code IN (
      'communication.guarantee.direct',
      'communication.guarantee.absolute',
      'communication.guarantee.case',
      'communication.guarantee.duration',
      'communication.misleading.exclusivity',
      'communication.misleading.disparage',
      'communication.fear.exaggeration',
      'communication.fear.panic',
      'communication.fear.compare'
    ))
  )
ON CONFLICT DO NOTHING;
