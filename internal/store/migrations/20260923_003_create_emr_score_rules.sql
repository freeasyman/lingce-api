-- 门诊病历评分规则拆分。
--
-- 完整评分标准保存在 emr_score_standards。
-- 本表只保存该标准的可执行拆分：
--   程序判断：空值、基础结构和确定性事实；
--   大模型判断：需要结合病历语义的内容。
--
-- 同一 duplicate_group 内的规则不得对同一事实重复扣分。

CREATE TABLE IF NOT EXISTS public.emr_score_rules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  standard_id UUID NOT NULL REFERENCES public.emr_score_standards(id) ON DELETE CASCADE,
  rule_code VARCHAR(160) NOT NULL,
  section_code VARCHAR(64) NOT NULL,
  rule_name VARCHAR(255) NOT NULL,
  execution_mode VARCHAR(16) NOT NULL,
  standard_basis TEXT NOT NULL DEFAULT '',
  precondition TEXT NOT NULL DEFAULT '',
  evaluation_basis TEXT NOT NULL DEFAULT '',
  deduct_points INTEGER NOT NULL DEFAULT 0,
  deduct_policy VARCHAR(16) NOT NULL DEFAULT 'fixed',
  duplicate_group VARCHAR(160) NOT NULL DEFAULT '',
  allow_auto_deduct BOOLEAN NOT NULL DEFAULT TRUE,
  needs_manual_review BOOLEAN NOT NULL DEFAULT FALSE,
  status VARCHAR(16) NOT NULL DEFAULT 'published',
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT emr_score_rules_execution_mode_check
    CHECK (execution_mode IN ('程序判断', '大模型判断')),
  CONSTRAINT emr_score_rules_deduct_policy_check
    CHECK (deduct_policy IN ('fixed', 'per_item', 'manual')),
  CONSTRAINT emr_score_rules_status_check
    CHECK (status IN ('draft', 'published', 'disabled')),
  CONSTRAINT emr_score_rules_deduct_points_check
    CHECK (deduct_points >= 0),
  CONSTRAINT emr_score_rules_review_check
    CHECK (allow_auto_deduct OR needs_manual_review)
);

CREATE UNIQUE INDEX IF NOT EXISTS emr_score_rules_standard_code_uq
  ON public.emr_score_rules (standard_id, rule_code);

CREATE INDEX IF NOT EXISTS emr_score_rules_standard_execution_idx
  ON public.emr_score_rules (standard_id, status, execution_mode, section_code, sort_order);

CREATE INDEX IF NOT EXISTS emr_score_rules_duplicate_group_idx
  ON public.emr_score_rules (standard_id, duplicate_group)
  WHERE duplicate_group <> '';

WITH standard AS (
  SELECT id
  FROM public.emr_score_standards
  WHERE code = 'outpatient_record_quality'
    AND version = '2017'
)
INSERT INTO public.emr_score_rules
(
  standard_id,
  rule_code,
  section_code,
  rule_name,
  execution_mode,
  standard_basis,
  precondition,
  evaluation_basis,
  deduct_points,
  deduct_policy,
  duplicate_group,
  allow_auto_deduct,
  needs_manual_review,
  status,
  sort_order
)
SELECT standard.id, seed.rule_code, seed.section_code, seed.rule_name, seed.execution_mode,
       seed.standard_basis, seed.precondition, seed.evaluation_basis, seed.deduct_points,
       seed.deduct_policy, seed.duplicate_group, seed.allow_auto_deduct,
       seed.needs_manual_review, 'published', seed.sort_order
FROM standard
CROSS JOIN (
  VALUES
  ('general.missing_patient_information', 'general', '一般项目医疗信息缺失', '程序判断',
   '一般项目：各项医疗信息未填写一项，扣2分。', '对应患者基本信息字段已由系统采集。',
   '只判断字段是否存在有效值；不判断医学内容是否完整。', 2, 'per_item', 'general.patient_information_missing', TRUE, FALSE, 10),
  ('general.missing_visit_date', 'general', '就诊日期缺失', '程序判断',
   '一般项目：每次就诊应有就诊日期。', '', '只判断就诊日期是否存在。', 2, 'fixed', 'general.visit_date', TRUE, FALSE, 20),
  ('general.emergency_time_incomplete', 'general', '急诊时间未精确到分钟', '程序判断',
   '一般项目：急诊病例就诊日期应具体到分钟，扣3分。', '当前病历为急诊且已存在就诊日期。',
   '只判断时间是否精确到分钟。', 3, 'fixed', 'general.visit_date', TRUE, FALSE, 30),
  ('chief.missing', 'chief_complaint', '主诉缺失', '程序判断',
   '主诉：缺少主诉内容，扣5分。', '', '只判断主诉是否为空。', 5, 'fixed', 'chief.content_missing', TRUE, FALSE, 100),
  ('chief.too_short', 'chief_complaint', '主诉内容过少', '程序判断',
   '主诉：描述不确切、不完整或缺少某一部分，扣2分。', '主诉已有内容。',
   '程序只判断内容长度，不判断症状、部位和时间是否语义完整。', 2, 'fixed', 'chief.semantic_completeness', TRUE, FALSE, 110),
  ('history.missing', 'history', '现病史缺失', '程序判断',
   '病史：缺现病史，扣20分。', '', '只判断现病史是否为空。', 20, 'fixed', 'history.content_missing', TRUE, FALSE, 200),
  ('history.too_short', 'history', '现病史内容过少', '程序判断',
   '病史：病史内容明显不足时按标准酌情扣分。', '现病史已有内容。',
   '程序只判断内容长度，不判断病史是否围绕主诉或记录疾病演变。', 4, 'fixed', 'history.semantic_completeness', TRUE, FALSE, 210),
  ('exam.missing', 'physical_exam', '体格检查缺失', '程序判断',
   '体检：缺少必要的体检项目，扣20分。', '', '只判断体格检查是否为空。', 20, 'fixed', 'exam.content_missing', TRUE, FALSE, 300),
  ('exam.too_short', 'physical_exam', '体格检查内容过少', '程序判断',
   '体检：记录不准确、不全面，扣5分。', '体格检查已有内容。',
   '程序只判断内容长度，不判断阳性、阴性和专科体征是否必要。', 5, 'fixed', 'exam.semantic_completeness', TRUE, FALSE, 310),
  ('diagnosis.missing', 'diagnosis', '诊断缺失', '程序判断',
   '诊断：未明确诊断病历没有进一步检查意见和建议，扣10分。', '', '只判断诊断字段是否为空。', 10, 'fixed', 'diagnosis.content_missing', TRUE, FALSE, 400),
  ('treatment.missing', 'treatment', '处理治疗缺失', '程序判断',
   '处理治疗：缺治疗意见，扣20分。', '', '处理、处置、医嘱和处方均为空时扣分。', 20, 'fixed', 'treatment.content_missing', TRUE, FALSE, 500),
  ('treatment.too_short', 'treatment', '处理治疗内容过少', '程序判断',
   '处理治疗：治疗措施和处方应有明确记录。', '处理治疗已有内容。',
   '程序只判断内容长度，不判断治疗合理性和处方医嘱一致性。', 3, 'fixed', 'treatment.semantic_completeness', TRUE, FALSE, 510),
  ('signature.missing', 'signature', '医师签名缺失', '程序判断',
   '医师签名：医师未签名，扣5分。', '', '只接受正式电子签名字段，不把正文姓名或保存操作人当作签名。', 5, 'fixed', 'signature.missing', TRUE, FALSE, 600),

  ('chief.incomplete_semantics', 'chief_complaint', '主诉缺少必要要素', '大模型判断',
   '主诉：内容完整，包括主要症状、部位和时间；缺少某一部分，扣2分。', '主诉不为空且不是允许的“病史同前”。',
   '判断主诉是否表达主要症状、适用的部位和持续时间；不重复判断主诉是否为空。', 2, 'fixed', 'chief.semantic_completeness', TRUE, FALSE, 1000),
  ('chief.replaced_by_diagnosis', 'chief_complaint', '主诉被诊断替代', '大模型判断',
   '主诉：不得以诊断代替主诉，扣2分。', '主诉和诊断均有内容。',
   '判断主诉是否只是疾病名称或检查结论，未表达患者本次不适或就诊目的。', 2, 'fixed', 'chief.diagnosis_replacement', TRUE, FALSE, 1010),
  ('chief.followup_same_history_invalid', 'chief_complaint', '复诊“病史同前”不适用', '大模型判断',
   '主诉：有新症状体征而缺主诉的复诊、取药病历，扣3分。', '复诊或取药病历使用“病史同前”。',
   '判断病历是否存在新症状、体征、病情变化或方案调整；无变化时不得扣分。', 3, 'fixed', 'chief.followup_same_history', TRUE, FALSE, 1020),
  ('history.initial_not_related_to_chief', 'history', '初诊病史与主诉不相符', '大模型判断',
   '病史：初诊现病史应与主诉相关、相符，扣4分。', '初诊病历且主诉、现病史均有内容。',
   '判断现病史是否围绕主诉，不能因程序已经检查字段有值而重复扣分。', 4, 'fixed', 'history.relation_to_chief', TRUE, FALSE, 1100),
  ('history.initial_disorganized', 'history', '初诊病史重点不突出', '大模型判断',
   '病史：初诊现病史描述混乱、重点不突出，扣4分。', '初诊病历且现病史有足够文本。',
   '判断起始、演变、主要症状、诊疗经过和疗效是否能够被清楚识别。', 4, 'fixed', 'history.semantic_completeness', TRUE, FALSE, 1110),
  ('history.initial_missing_differential_basis', 'history', '初诊缺少主要鉴别诊断资料', '大模型判断',
   '病史：初诊缺少主要鉴别诊断资料，扣3分。', '初诊病历且病情需要鉴别判断。',
   '只在病情和当前诊断确实需要相关鉴别资料时判断；无法确认时不得自动扣分。', 3, 'fixed', 'history.differential_basis', TRUE, TRUE, 1120),
  ('history.followup_missing_change', 'history', '复诊缺少病情变化或疗效', '大模型判断',
   '病史：复诊未记录治疗后病情变化或治疗效果，扣5分。', '复诊病历且存在既往治疗或复诊背景。',
   '判断是否记录病情变化、治疗效果或无变化的明确说明。', 5, 'fixed', 'history.followup_change', TRUE, FALSE, 1130),
  ('history.related_past_history_insufficient', 'history', '相关既往史等记录不足', '大模型判断',
   '病史：与本次疾病相关的既往史及其他病史有重要不足，每项扣3分。', '病情需要相关既往史、婚育史、过敏史或家族史。',
   '只判断与本次疾病相关且确有必要的病史，不把所有空栏位都视为缺失。', 3, 'per_item', 'history.related_background', TRUE, TRUE, 1140),
  ('exam.incomplete_or_inaccurate', 'physical_exam', '体检记录不准确或不全面', '大模型判断',
   '体检：记录不准确、不全面，扣5分。', '体格检查已有内容。',
   '判断体检是否覆盖与主诉、诊断相关的必要项目；不重复判断体检是否为空。', 5, 'fixed', 'exam.semantic_completeness', TRUE, FALSE, 1200),
  ('exam.missing_key_signs', 'physical_exam', '未记录重要阳性或阴性体征', '大模型判断',
   '体检：阳性体征或主要阴性体征未描述，扣5分。', '主诉或诊断对相关体征有明确要求。',
   '根据当前病情判断是否遗漏有诊断意义的阳性或阴性体征；不凭常识泛化。', 5, 'fixed', 'exam.key_signs', TRUE, TRUE, 1210),
  ('exam.missing_specialty_exam', 'physical_exam', '缺少必要专科检查', '大模型判断',
   '体检：缺少专科检查记录，扣10分。', '当前科室或病情明确需要专科检查。',
   '只在专科检查必要性明确时判断，否则返回无法判断并要求人工复核。', 10, 'fixed', 'exam.specialty_exam', TRUE, TRUE, 1220),
  ('auxiliary.missing_required_exam', 'auxiliary_exam', '缺少必要辅助检查', '大模型判断',
   '辅助检查：初诊或复诊必需的检查缺一项，扣2分。', '当前病情、诊断或复诊背景明确需要该检查。',
   '判断检查是否确为当前病情所必需；不能因为没有检查就直接推断必须检查。', 2, 'per_item', 'auxiliary.required_exam', TRUE, TRUE, 1300),
  ('auxiliary.result_not_recorded', 'auxiliary_exam', '相关检查结果未记录', '大模型判断',
   '辅助检查：与诊断和鉴别诊断相关的检查结果未抄写，扣3分。', '病历中提到已做检查或检查结果存在来源。',
   '判断已知检查结果是否在病历正文中记录；不判断未做检查。', 3, 'fixed', 'auxiliary.result_record', TRUE, FALSE, 1310),
  ('diagnosis.incomplete_or_irregular', 'diagnosis', '诊断名称不全或不规范', '大模型判断',
   '诊断：诊断名称不全、不规范，扣5分。', '诊断字段有内容。',
   '判断诊断是否为完整、规范的诊断名称；不重复判断诊断字段是否为空。', 5, 'fixed', 'diagnosis.semantic_quality', TRUE, FALSE, 1400),
  ('diagnosis.missing_followup_plan', 'diagnosis', '待查诊断缺少进一步检查建议', '大模型判断',
   '诊断：未明确诊断病历没有进一步检查意见和建议，扣10分。', '诊断包含待查、待诊或未明确诊断。',
   '判断是否提出与当前病情相关的进一步检查、建议或首先考虑的可能诊断。', 10, 'fixed', 'diagnosis.pending_plan', TRUE, FALSE, 1410),
  ('treatment.unreasonable', 'treatment', '治疗措施不合理、不正确或不及时', '大模型判断',
   '处理治疗：各项治疗措施不合理、不正确或不及时，每项扣3分。', '处理治疗有内容且包含具体措施。',
   '发现疑点时必须提供证据；涉及医疗安全或严重不合理时只标记人工复核。', 3, 'per_item', 'treatment.reasonableness', FALSE, TRUE, 1500),
  ('treatment.prescription_incomplete', 'treatment', '处方信息不完整', '大模型判断',
   '处理治疗：处方应写药物全称、用量、用法等；处方书写不符合规则，扣3分。', '病历中存在处方或药物治疗内容。',
   '判断药物全称、用量、用法等是否足以理解；不重复判断处理治疗是否为空。', 3, 'fixed', 'treatment.prescription_completeness', TRUE, FALSE, 1510),
  ('treatment.prescription_advice_inconsistent', 'treatment', '处方与医嘱不相符', '大模型判断',
   '处理治疗：处方与病历中医嘱不相符，扣5分。', '处方和医嘱均有可比较内容。',
   '比较药物、剂量、用法、频次和疗程；无法比较时不扣分。', 5, 'fixed', 'treatment.prescription_advice_relation', TRUE, FALSE, 1520),
  ('writing.electronic_record_issue', 'writing', '电子病历书写规范问题', '大模型判断',
   '病历书写要求：文字不清、随意书写或不合规修改，每项扣3分。', '当前输入提供了可识别的书写或修改信息。',
   '只判断输入中明确存在的电子书写问题；没有证据时不得扣分。', 3, 'per_item', 'writing.electronic_quality', FALSE, TRUE, 1600)
) AS seed(
  rule_code, section_code, rule_name, execution_mode, standard_basis,
  precondition, evaluation_basis, deduct_points, deduct_policy, duplicate_group,
  allow_auto_deduct, needs_manual_review, sort_order
)
ON CONFLICT (standard_id, rule_code) DO UPDATE SET
  section_code = EXCLUDED.section_code,
  rule_name = EXCLUDED.rule_name,
  execution_mode = EXCLUDED.execution_mode,
  standard_basis = EXCLUDED.standard_basis,
  precondition = EXCLUDED.precondition,
  evaluation_basis = EXCLUDED.evaluation_basis,
  deduct_points = EXCLUDED.deduct_points,
  deduct_policy = EXCLUDED.deduct_policy,
  duplicate_group = EXCLUDED.duplicate_group,
  allow_auto_deduct = EXCLUDED.allow_auto_deduct,
  needs_manual_review = EXCLUDED.needs_manual_review,
  status = EXCLUDED.status,
  sort_order = EXCLUDED.sort_order,
  updated_at = NOW();

DO $$
BEGIN
  IF (
    SELECT COUNT(*)
    FROM public.emr_score_rules rule
    JOIN public.emr_score_standards standard ON standard.id = rule.standard_id
    WHERE standard.code = 'outpatient_record_quality'
      AND standard.version = '2017'
      AND rule.status = 'published'
  ) < 30 THEN
    RAISE EXCEPTION '门诊病历评分规则拆分写入不完整';
  END IF;
END $$;
