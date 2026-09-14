-- 初始化平台默认五套门诊模板的质量规则绑定。
--
-- 对应关系：
--   template_code      -> emr_templates.code（仅选择 status=published 且 is_current=true 的版本行）
--   quality_rule_code  -> emr_quality_rules.code（平台默认 tenant_id=0）
--
-- 本迁移只处理 tenant_id=0 的平台默认绑定，不覆盖租户自定义绑定。
-- 对平台默认五套模板先清理后重建绑定，保证重复执行结果确定，且不依赖绑定表
-- 是否存在可用于 ON CONFLICT 的唯一约束。
--
-- 署名：Codex
-- 时间：2026-09-14

-- 旧表约束未包含“模板发布前处理”，但当前规则种子明确使用该处理时点。
-- 先删除旧的截止动作检查约束，再安装包含全部已确认枚举值的新约束。
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    IF to_regclass('public.emr_template_quality_rules') IS NULL THEN
        RAISE EXCEPTION '缺少表 public.emr_template_quality_rules，无法导入模板质量规则绑定';
    END IF;

    FOR constraint_name IN
        SELECT c.conname
        FROM pg_constraint c
        JOIN pg_class r ON r.oid = c.conrelid
        JOIN pg_namespace n ON n.oid = r.relnamespace
        WHERE n.nspname = 'public'
          AND r.relname = 'emr_template_quality_rules'
          AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) ILIKE '%deadline_action%'
    LOOP
        EXECUTE format(
            'ALTER TABLE public.emr_template_quality_rules DROP CONSTRAINT %I',
            constraint_name
        );
    END LOOP;
END $$;

ALTER TABLE public.emr_template_quality_rules
    ADD CONSTRAINT emr_template_quality_deadline_check
    CHECK (deadline_action IN (
        '仅提示', '提交前处理', '确认前处理', '归档前处理',
        '归档后质控', '模板发布前处理'
    ));

CREATE TEMP TABLE _emr_seed_template_quality_rules (
    template_code TEXT NOT NULL,
    quality_rule_code TEXT NOT NULL,
    is_enabled BOOLEAN NOT NULL,
    execution_mode TEXT NOT NULL,
    deadline_action TEXT NOT NULL,
    display_order INTEGER NOT NULL
) ON COMMIT DROP;

INSERT INTO _emr_seed_template_quality_rules
    (template_code, quality_rule_code, is_enabled, execution_mode, deadline_action, display_order)
VALUES
    ('internal_medicine', 'emr.patient.basic_info.name', TRUE, '程序判断', '归档前处理', 10),
    ('internal_medicine', 'emr.patient.basic_info.gender', TRUE, '程序判断', '归档前处理', 20),
    ('internal_medicine', 'emr.patient.basic_info.birth_or_age', TRUE, '程序判断', '归档前处理', 30),
    ('internal_medicine', 'emr.patient.basic_info.phone', TRUE, '程序判断', '归档前处理', 40),
    ('internal_medicine', 'emr.patient.basic_info.address', TRUE, '程序判断', '归档前处理', 50),
    ('internal_medicine', 'emr.patient.basic_info.ethnicity', TRUE, '程序判断', '归档前处理', 60),
    ('internal_medicine', 'emr.patient.basic_info.marital_status', TRUE, '程序判断', '归档前处理', 70),
    ('internal_medicine', 'emr.patient.basic_info.occupation', TRUE, '程序判断', '归档前处理', 80),
    ('internal_medicine', 'emr.patient.basic_info.encounter_time', TRUE, '程序判断', '归档前处理', 90),
    ('internal_medicine', 'emr.patient.basic_info.department', TRUE, '程序判断', '归档前处理', 100),
    ('internal_medicine', 'emr.patient.basic_info.attending_doctor', TRUE, '程序判断', '归档前处理', 110),
    ('internal_medicine', 'emr.patient.basic_info.pending_confirmation', TRUE, '程序判断', '归档前处理', 120),
    ('internal_medicine', 'emr.time.consistency.fields_present', TRUE, '程序判断', '归档后质控', 130),
    ('internal_medicine', 'emr.time.consistency.order', TRUE, '程序判断', '归档后质控', 140),
    ('internal_medicine', 'emr.time.consistency.last_modified', TRUE, '程序判断', '归档后质控', 150),
    ('internal_medicine', 'emr.time.consistency.abnormal_span', TRUE, '程序判断', '归档后质控', 160),
    ('internal_medicine', 'emr.chief_complaint.required', TRUE, '程序判断', '归档后质控', 170),
    ('internal_medicine', 'emr.chief_complaint.max_length', TRUE, '程序判断', '归档后质控', 180),
    ('internal_medicine', 'emr.chief_complaint.duration', TRUE, '大模型判断', '归档后质控', 190),
    ('internal_medicine', 'emr.chief_complaint.symptom_sign', TRUE, '大模型判断', '归档后质控', 200),
    ('internal_medicine', 'emr.chief_complaint.diagnosis_only', TRUE, '大模型判断', '归档后质控', 210),
    ('internal_medicine', 'emr.chief_complaint.not_vague', TRUE, '大模型判断', '归档后质控', 220),
    ('internal_medicine', 'emr.chief_complaint.consistency', TRUE, '大模型判断', '归档后质控', 230),
    ('internal_medicine', 'emr.present_illness.focus', TRUE, '大模型判断', '归档后质控', 240),
    ('internal_medicine', 'emr.present_illness.onset', TRUE, '大模型判断', '归档后质控', 250),
    ('internal_medicine', 'emr.present_illness.evolution', TRUE, '大模型判断', '归档后质控', 260),
    ('internal_medicine', 'emr.present_illness.associated_symptoms', TRUE, '大模型判断', '归档后质控', 270),
    ('internal_medicine', 'emr.present_illness.aggravating_relief', TRUE, '大模型判断', '归档后质控', 280),
    ('internal_medicine', 'emr.present_illness.prior_treatment', TRUE, '大模型判断', '归档后质控', 290),
    ('internal_medicine', 'emr.present_illness.treatment_effect', TRUE, '大模型判断', '归档后质控', 300),
    ('internal_medicine', 'emr.present_illness.followup_change', TRUE, '大模型判断', '归档后质控', 310),
    ('internal_medicine', 'emr.present_illness.general_status.diet', TRUE, '人工判断', '提交前处理', 320),
    ('internal_medicine', 'emr.present_illness.general_status.sleep', TRUE, '人工判断', '提交前处理', 330),
    ('internal_medicine', 'emr.present_illness.general_status.bowel', TRUE, '人工判断', '提交前处理', 340),
    ('internal_medicine', 'emr.present_illness.general_status.urination', TRUE, '人工判断', '提交前处理', 350),
    ('internal_medicine', 'emr.present_illness.general_status.weight_change', TRUE, '人工判断', '提交前处理', 360),
    ('internal_medicine', 'emr.past_history.chronic', TRUE, '大模型判断', '归档后质控', 370),
    ('internal_medicine', 'emr.past_history.surgery', TRUE, '人工判断', '归档前处理', 380),
    ('internal_medicine', 'emr.past_history.trauma', TRUE, '人工判断', '归档前处理', 390),
    ('internal_medicine', 'emr.past_history.infectious', TRUE, '人工判断', '归档前处理', 400),
    ('internal_medicine', 'emr.past_history.same_condition', TRUE, '人工判断', '归档前处理', 410),
    ('internal_medicine', 'emr.past_history.prior_treatment', TRUE, '大模型判断', '归档后质控', 420),
    ('internal_medicine', 'emr.personal_history.smoking', TRUE, '人工判断', '归档后质控', 430),
    ('internal_medicine', 'emr.personal_history.alcohol', TRUE, '人工判断', '归档后质控', 440),
    ('internal_medicine', 'emr.personal_history.exposure', TRUE, '人工判断', '归档后质控', 450),
    ('internal_medicine', 'emr.personal_history.lifestyle', TRUE, '人工判断', '归档后质控', 460),
    ('internal_medicine', 'emr.family_history.hereditary', TRUE, '人工判断', '归档后质控', 470),
    ('internal_medicine', 'emr.family_history.tumor', TRUE, '人工判断', '归档后质控', 480),
    ('internal_medicine', 'emr.family_history.chronic', TRUE, '人工判断', '归档后质控', 490),
    ('internal_medicine', 'emr.allergy.required.status', TRUE, '程序判断', '归档前处理', 500),
    ('internal_medicine', 'emr.allergy.required.none_evidence', TRUE, '人工判断', '归档前处理', 510),
    ('internal_medicine', 'emr.allergy.required.drug_detail', TRUE, '程序判断', '归档前处理', 520),
    ('internal_medicine', 'emr.allergy.required.other_detail', TRUE, '程序判断', '归档前处理', 530),
    ('internal_medicine', 'emr.allergy.required.unconfirmed', TRUE, '程序判断', '归档前处理', 540),
    ('internal_medicine', 'emr.allergy.prescription_risk.known_allergy', TRUE, '程序判断', '归档后质控', 550),
    ('internal_medicine', 'emr.allergy.prescription_risk.manual_confirmation', TRUE, '人工判断', '归档后质控', 560),
    ('internal_medicine', 'emr.physical_exam.vital_signs', TRUE, '程序判断', '归档后质控', 570),
    ('internal_medicine', 'emr.physical_exam.positive_signs', TRUE, '大模型判断', '归档后质控', 580),
    ('internal_medicine', 'emr.physical_exam.negative_signs', TRUE, '大模型判断', '归档后质控', 590),
    ('internal_medicine', 'emr.physical_exam.specialty', TRUE, '大模型判断', '归档后质控', 600),
    ('internal_medicine', 'emr.physical_exam.not_generic', TRUE, '大模型判断', '归档后质控', 610),
    ('internal_medicine', 'emr.auxiliary_exam.item_recorded', TRUE, '程序判断', '归档后质控', 620),
    ('internal_medicine', 'emr.auxiliary_exam.result_recorded', TRUE, '程序判断', '归档后质控', 630),
    ('internal_medicine', 'emr.auxiliary_exam.conclusion_recorded', TRUE, '人工判断', '归档后质控', 640),
    ('internal_medicine', 'emr.auxiliary_exam.pending_explicit', TRUE, '程序判断', '归档后质控', 650),
    ('internal_medicine', 'emr.auxiliary_exam.external_confirmed', TRUE, '人工判断', '归档后质控', 660),
    ('internal_medicine', 'emr.diagnosis.present', TRUE, '程序判断', '归档后质控', 670),
    ('internal_medicine', 'emr.diagnosis.standard_name', TRUE, '大模型判断', '归档后质控', 680),
    ('internal_medicine', 'emr.diagnosis.pending', TRUE, '大模型判断', '归档后质控', 690),
    ('internal_medicine', 'emr.diagnosis.followup_plan', TRUE, '程序判断', '归档后质控', 700),
    ('internal_medicine', 'emr.diagnosis.consistency.evidence_chain', TRUE, '大模型判断', '归档后质控', 710),
    ('internal_medicine', 'emr.diagnosis.consistency.history', TRUE, '大模型判断', '归档后质控', 720),
    ('internal_medicine', 'emr.diagnosis.consistency.exam', TRUE, '大模型判断', '归档后质控', 730),
    ('internal_medicine', 'emr.treatment.consistency.prescription', TRUE, '大模型判断', '归档后质控', 740),
    ('internal_medicine', 'emr.treatment.consistency.disposition', TRUE, '大模型判断', '归档后质控', 750),
    ('internal_medicine', 'emr.treatment.consistency.advice', TRUE, '大模型判断', '归档后质控', 760),
    ('internal_medicine', 'emr.treatment.consistency.recorded', TRUE, '程序判断', '归档后质控', 770),
    ('internal_medicine', 'emr.treatment.consistency.followup', TRUE, '人工判断', '归档后质控', 780),
    ('internal_medicine', 'emr.followup.time', TRUE, '人工判断', '归档前处理', 790),
    ('internal_medicine', 'emr.followup.trigger', TRUE, '人工判断', '归档前处理', 800),
    ('internal_medicine', 'emr.followup.requirement', TRUE, '人工判断', '归档前处理', 810),
    ('internal_medicine', 'emr.signature.doctor', TRUE, '程序判断', '归档前处理', 820),
    ('internal_medicine', 'emr.signature.supervisor', TRUE, '程序判断', '归档前处理', 830),
    ('internal_medicine', 'emr.audit.trail.operator', TRUE, '程序判断', '归档后质控', 840),
    ('internal_medicine', 'emr.audit.trail.time', TRUE, '程序判断', '归档后质控', 850),
    ('internal_medicine', 'emr.audit.trail.reason', TRUE, '程序判断', '归档后质控', 860),
    ('internal_medicine', 'emr.audit.trail.before_value', TRUE, '程序判断', '归档后质控', 870),
    ('internal_medicine', 'emr.audit.trail.after_value', TRUE, '程序判断', '归档后质控', 880),
    ('internal_medicine', 'common.visible_fields.chief_complaint', TRUE, '程序判断', '模板发布前处理', 890),
    ('internal_medicine', 'common.visible_fields.present_illness', TRUE, '程序判断', '模板发布前处理', 900),
    ('internal_medicine', 'common.visible_fields.past_history', TRUE, '程序判断', '模板发布前处理', 910),
    ('internal_medicine', 'common.visible_fields.personal_history', TRUE, '程序判断', '模板发布前处理', 920),
    ('internal_medicine', 'common.visible_fields.family_history', TRUE, '程序判断', '模板发布前处理', 930),
    ('internal_medicine', 'common.visible_fields.allergy_history', TRUE, '程序判断', '模板发布前处理', 940),
    ('internal_medicine', 'common.visible_fields.physical_exam', TRUE, '程序判断', '模板发布前处理', 950),
    ('internal_medicine', 'common.visible_fields.auxiliary_exam', TRUE, '程序判断', '模板发布前处理', 960),
    ('internal_medicine', 'common.visible_fields.diagnosis', TRUE, '程序判断', '模板发布前处理', 970),
    ('internal_medicine', 'common.visible_fields.prescription', TRUE, '程序判断', '模板发布前处理', 980),
    ('internal_medicine', 'common.visible_fields.disposition', TRUE, '程序判断', '模板发布前处理', 990),
    ('internal_medicine', 'common.visible_fields.medical_advice', TRUE, '程序判断', '模板发布前处理', 1000),
    ('internal_medicine', 'common.visible_fields.followup', TRUE, '程序判断', '模板发布前处理', 1010),
    ('internal_medicine', 'common.visible_fields.supplement', TRUE, '程序判断', '模板发布前处理', 1020),
    ('internal_medicine', 'common.orders_split.prescription', TRUE, '程序判断', '模板发布前处理', 1030),
    ('internal_medicine', 'common.orders_split.treatment', TRUE, '程序判断', '模板发布前处理', 1040),
    ('internal_medicine', 'common.orders_split.exam', TRUE, '程序判断', '模板发布前处理', 1050),
    ('internal_medicine', 'common.orders_split.infusion', TRUE, '程序判断', '模板发布前处理', 1060),
    ('internal_medicine', 'common.orders_split.procedure', TRUE, '程序判断', '模板发布前处理', 1070),
    ('tcm', 'emr.patient.basic_info.name', TRUE, '程序判断', '归档前处理', 10),
    ('tcm', 'emr.patient.basic_info.gender', TRUE, '程序判断', '归档前处理', 20),
    ('tcm', 'emr.patient.basic_info.birth_or_age', TRUE, '程序判断', '归档前处理', 30),
    ('tcm', 'emr.patient.basic_info.phone', TRUE, '程序判断', '归档前处理', 40),
    ('tcm', 'emr.patient.basic_info.address', TRUE, '程序判断', '归档前处理', 50),
    ('tcm', 'emr.patient.basic_info.ethnicity', TRUE, '程序判断', '归档前处理', 60),
    ('tcm', 'emr.patient.basic_info.marital_status', TRUE, '程序判断', '归档前处理', 70),
    ('tcm', 'emr.patient.basic_info.occupation', TRUE, '程序判断', '归档前处理', 80),
    ('tcm', 'emr.patient.basic_info.encounter_time', TRUE, '程序判断', '归档前处理', 90),
    ('tcm', 'emr.patient.basic_info.department', TRUE, '程序判断', '归档前处理', 100),
    ('tcm', 'emr.patient.basic_info.attending_doctor', TRUE, '程序判断', '归档前处理', 110),
    ('tcm', 'emr.patient.basic_info.pending_confirmation', TRUE, '程序判断', '归档前处理', 120),
    ('tcm', 'emr.time.consistency.fields_present', TRUE, '程序判断', '归档后质控', 130),
    ('tcm', 'emr.time.consistency.order', TRUE, '程序判断', '归档后质控', 140),
    ('tcm', 'emr.time.consistency.last_modified', TRUE, '程序判断', '归档后质控', 150),
    ('tcm', 'emr.time.consistency.abnormal_span', TRUE, '程序判断', '归档后质控', 160),
    ('tcm', 'emr.chief_complaint.required', TRUE, '程序判断', '归档后质控', 170),
    ('tcm', 'emr.chief_complaint.max_length', TRUE, '程序判断', '归档后质控', 180),
    ('tcm', 'emr.chief_complaint.duration', TRUE, '大模型判断', '归档后质控', 190),
    ('tcm', 'emr.chief_complaint.symptom_sign', TRUE, '大模型判断', '归档后质控', 200),
    ('tcm', 'emr.chief_complaint.diagnosis_only', TRUE, '大模型判断', '归档后质控', 210),
    ('tcm', 'emr.chief_complaint.not_vague', TRUE, '大模型判断', '归档后质控', 220),
    ('tcm', 'emr.chief_complaint.consistency', TRUE, '大模型判断', '归档后质控', 230),
    ('tcm', 'emr.present_illness.focus', TRUE, '大模型判断', '归档后质控', 240),
    ('tcm', 'emr.present_illness.onset', TRUE, '大模型判断', '归档后质控', 250),
    ('tcm', 'emr.present_illness.evolution', TRUE, '大模型判断', '归档后质控', 260),
    ('tcm', 'emr.present_illness.associated_symptoms', TRUE, '大模型判断', '归档后质控', 270),
    ('tcm', 'emr.present_illness.aggravating_relief', TRUE, '大模型判断', '归档后质控', 280),
    ('tcm', 'emr.present_illness.prior_treatment', TRUE, '大模型判断', '归档后质控', 290),
    ('tcm', 'emr.present_illness.treatment_effect', TRUE, '大模型判断', '归档后质控', 300),
    ('tcm', 'emr.present_illness.followup_change', TRUE, '大模型判断', '归档后质控', 310),
    ('tcm', 'emr.present_illness.general_status.diet', TRUE, '人工判断', '提交前处理', 320),
    ('tcm', 'emr.present_illness.general_status.sleep', TRUE, '人工判断', '提交前处理', 330),
    ('tcm', 'emr.present_illness.general_status.bowel', TRUE, '人工判断', '提交前处理', 340),
    ('tcm', 'emr.present_illness.general_status.urination', TRUE, '人工判断', '提交前处理', 350),
    ('tcm', 'emr.present_illness.general_status.weight_change', TRUE, '人工判断', '提交前处理', 360),
    ('tcm', 'emr.past_history.chronic', TRUE, '大模型判断', '归档后质控', 370),
    ('tcm', 'emr.past_history.surgery', TRUE, '人工判断', '归档前处理', 380),
    ('tcm', 'emr.past_history.trauma', TRUE, '人工判断', '归档前处理', 390),
    ('tcm', 'emr.past_history.infectious', TRUE, '人工判断', '归档前处理', 400),
    ('tcm', 'emr.past_history.same_condition', TRUE, '人工判断', '归档前处理', 410),
    ('tcm', 'emr.past_history.prior_treatment', TRUE, '大模型判断', '归档后质控', 420),
    ('tcm', 'emr.personal_history.smoking', TRUE, '人工判断', '归档后质控', 430),
    ('tcm', 'emr.personal_history.alcohol', TRUE, '人工判断', '归档后质控', 440),
    ('tcm', 'emr.personal_history.exposure', TRUE, '人工判断', '归档后质控', 450),
    ('tcm', 'emr.personal_history.lifestyle', TRUE, '人工判断', '归档后质控', 460),
    ('tcm', 'emr.family_history.hereditary', TRUE, '人工判断', '归档后质控', 470),
    ('tcm', 'emr.family_history.tumor', TRUE, '人工判断', '归档后质控', 480),
    ('tcm', 'emr.family_history.chronic', TRUE, '人工判断', '归档后质控', 490),
    ('tcm', 'emr.allergy.required.status', TRUE, '程序判断', '归档前处理', 500),
    ('tcm', 'emr.allergy.required.none_evidence', TRUE, '人工判断', '归档前处理', 510),
    ('tcm', 'emr.allergy.required.drug_detail', TRUE, '程序判断', '归档前处理', 520),
    ('tcm', 'emr.allergy.required.other_detail', TRUE, '程序判断', '归档前处理', 530),
    ('tcm', 'emr.allergy.required.unconfirmed', TRUE, '程序判断', '归档前处理', 540),
    ('tcm', 'emr.allergy.prescription_risk.known_allergy', TRUE, '程序判断', '归档后质控', 550),
    ('tcm', 'emr.allergy.prescription_risk.manual_confirmation', TRUE, '人工判断', '归档后质控', 560),
    ('tcm', 'emr.physical_exam.vital_signs', TRUE, '程序判断', '归档后质控', 570),
    ('tcm', 'emr.physical_exam.positive_signs', TRUE, '大模型判断', '归档后质控', 580),
    ('tcm', 'emr.physical_exam.negative_signs', TRUE, '大模型判断', '归档后质控', 590),
    ('tcm', 'emr.physical_exam.specialty', TRUE, '大模型判断', '归档后质控', 600),
    ('tcm', 'emr.physical_exam.not_generic', TRUE, '大模型判断', '归档后质控', 610),
    ('tcm', 'emr.auxiliary_exam.item_recorded', TRUE, '程序判断', '归档后质控', 620),
    ('tcm', 'emr.auxiliary_exam.result_recorded', TRUE, '程序判断', '归档后质控', 630),
    ('tcm', 'emr.auxiliary_exam.conclusion_recorded', TRUE, '人工判断', '归档后质控', 640),
    ('tcm', 'emr.auxiliary_exam.pending_explicit', TRUE, '程序判断', '归档后质控', 650),
    ('tcm', 'emr.auxiliary_exam.external_confirmed', TRUE, '人工判断', '归档后质控', 660),
    ('tcm', 'emr.diagnosis.present', TRUE, '程序判断', '归档后质控', 670),
    ('tcm', 'emr.diagnosis.standard_name', TRUE, '大模型判断', '归档后质控', 680),
    ('tcm', 'emr.diagnosis.pending', TRUE, '大模型判断', '归档后质控', 690),
    ('tcm', 'emr.diagnosis.followup_plan', TRUE, '程序判断', '归档后质控', 700),
    ('tcm', 'emr.diagnosis.consistency.evidence_chain', TRUE, '大模型判断', '归档后质控', 710),
    ('tcm', 'emr.diagnosis.consistency.history', TRUE, '大模型判断', '归档后质控', 720),
    ('tcm', 'emr.diagnosis.consistency.exam', TRUE, '大模型判断', '归档后质控', 730),
    ('tcm', 'emr.treatment.consistency.prescription', TRUE, '大模型判断', '归档后质控', 740),
    ('tcm', 'emr.treatment.consistency.disposition', TRUE, '大模型判断', '归档后质控', 750),
    ('tcm', 'emr.treatment.consistency.advice', TRUE, '大模型判断', '归档后质控', 760),
    ('tcm', 'emr.treatment.consistency.recorded', TRUE, '程序判断', '归档后质控', 770),
    ('tcm', 'emr.treatment.consistency.followup', TRUE, '人工判断', '归档后质控', 780),
    ('tcm', 'emr.followup.time', TRUE, '人工判断', '归档前处理', 790),
    ('tcm', 'emr.followup.trigger', TRUE, '人工判断', '归档前处理', 800),
    ('tcm', 'emr.followup.requirement', TRUE, '人工判断', '归档前处理', 810),
    ('tcm', 'emr.signature.doctor', TRUE, '程序判断', '归档前处理', 820),
    ('tcm', 'emr.signature.supervisor', TRUE, '程序判断', '归档前处理', 830),
    ('tcm', 'emr.audit.trail.operator', TRUE, '程序判断', '归档后质控', 840),
    ('tcm', 'emr.audit.trail.time', TRUE, '程序判断', '归档后质控', 850),
    ('tcm', 'emr.audit.trail.reason', TRUE, '程序判断', '归档后质控', 860),
    ('tcm', 'emr.audit.trail.before_value', TRUE, '程序判断', '归档后质控', 870),
    ('tcm', 'emr.audit.trail.after_value', TRUE, '程序判断', '归档后质控', 880),
    ('tcm', 'emr.tcm.elements.tcm_diagnosis', TRUE, '程序判断', '归档后质控', 890),
    ('tcm', 'emr.tcm.elements.wang', TRUE, '大模型判断', '归档后质控', 900),
    ('tcm', 'emr.tcm.elements.wen', TRUE, '大模型判断', '归档后质控', 910),
    ('tcm', 'emr.tcm.elements.wen_query', TRUE, '大模型判断', '归档后质控', 920),
    ('tcm', 'emr.tcm.elements.qie', TRUE, '大模型判断', '归档后质控', 930),
    ('tcm', 'emr.tcm.elements.tongue', TRUE, '大模型判断', '归档后质控', 940),
    ('tcm', 'emr.tcm.elements.pulse', TRUE, '大模型判断', '归档后质控', 950),
    ('tcm', 'emr.tcm.elements.syndrome', TRUE, '大模型判断', '归档后质控', 960),
    ('tcm', 'emr.tcm.elements.treatment_method', TRUE, '大模型判断', '归档后质控', 970),
    ('tcm', 'emr.tcm.elements.formula_or_care', TRUE, '大模型判断', '归档后质控', 980),
    ('tcm', 'emr.tcm.elements.location_allowed', TRUE, '程序判断', '归档前处理', 990),
    ('tcm', 'emr.tcm.evidence_chain.syndrome_to_four', TRUE, '大模型判断', '归档后质控', 1000),
    ('tcm', 'emr.tcm.evidence_chain.syndrome_to_tongue_pulse', TRUE, '大模型判断', '归档后质控', 1010),
    ('tcm', 'emr.tcm.evidence_chain.treatment_to_syndrome', TRUE, '大模型判断', '归档后质控', 1020),
    ('tcm', 'emr.tcm.western_diagnosis_prompt.present', TRUE, '程序判断', '归档后质控', 1030),
    ('tcm', 'emr.tcm.formula_consistency.formula', TRUE, '大模型判断', '归档后质控', 1040),
    ('tcm', 'emr.tcm.formula_consistency.modification', TRUE, '大模型判断', '归档后质控', 1050),
    ('tcm', 'emr.tcm.formula_consistency.decoction', TRUE, '大模型判断', '归档后质控', 1060),
    ('tcm', 'emr.tcm.formula_consistency.care', TRUE, '大模型判断', '归档后质控', 1070),
    ('tcm', 'common.visible_fields.chief_complaint', TRUE, '程序判断', '模板发布前处理', 1080),
    ('tcm', 'common.visible_fields.present_illness', TRUE, '程序判断', '模板发布前处理', 1090),
    ('tcm', 'common.visible_fields.past_history', TRUE, '程序判断', '模板发布前处理', 1100),
    ('tcm', 'common.visible_fields.personal_history', TRUE, '程序判断', '模板发布前处理', 1110),
    ('tcm', 'common.visible_fields.family_history', TRUE, '程序判断', '模板发布前处理', 1120),
    ('tcm', 'common.visible_fields.allergy_history', TRUE, '程序判断', '模板发布前处理', 1130),
    ('tcm', 'common.visible_fields.physical_exam', TRUE, '程序判断', '模板发布前处理', 1140),
    ('tcm', 'common.visible_fields.auxiliary_exam', TRUE, '程序判断', '模板发布前处理', 1150),
    ('tcm', 'common.visible_fields.diagnosis', TRUE, '程序判断', '模板发布前处理', 1160),
    ('tcm', 'common.visible_fields.prescription', TRUE, '程序判断', '模板发布前处理', 1170),
    ('tcm', 'common.visible_fields.disposition', TRUE, '程序判断', '模板发布前处理', 1180),
    ('tcm', 'common.visible_fields.medical_advice', TRUE, '程序判断', '模板发布前处理', 1190),
    ('tcm', 'common.visible_fields.followup', TRUE, '程序判断', '模板发布前处理', 1200),
    ('tcm', 'common.visible_fields.supplement', TRUE, '程序判断', '模板发布前处理', 1210),
    ('tcm', 'common.orders_split.prescription', TRUE, '程序判断', '模板发布前处理', 1220),
    ('tcm', 'common.orders_split.treatment', TRUE, '程序判断', '模板发布前处理', 1230),
    ('tcm', 'common.orders_split.exam', TRUE, '程序判断', '模板发布前处理', 1240),
    ('tcm', 'common.orders_split.infusion', TRUE, '程序判断', '模板发布前处理', 1250),
    ('tcm', 'common.orders_split.procedure', TRUE, '程序判断', '模板发布前处理', 1260),
    ('tcm', 'tcm.dual_diagnosis.western_slot', TRUE, '程序判断', '模板发布前处理', 1270),
    ('tcm', 'tcm.dual_diagnosis.tcm_slot', TRUE, '程序判断', '模板发布前处理', 1280),
    ('tcm', 'tcm.formula.prescription_object', TRUE, '程序判断', '模板发布前处理', 1290),
    ('tcm', 'tcm.formula.extension', TRUE, '程序判断', '模板发布前处理', 1300),
    ('gynecology', 'emr.patient.basic_info.name', TRUE, '程序判断', '归档前处理', 10),
    ('gynecology', 'emr.patient.basic_info.gender', TRUE, '程序判断', '归档前处理', 20),
    ('gynecology', 'emr.patient.basic_info.birth_or_age', TRUE, '程序判断', '归档前处理', 30),
    ('gynecology', 'emr.patient.basic_info.phone', TRUE, '程序判断', '归档前处理', 40),
    ('gynecology', 'emr.patient.basic_info.address', TRUE, '程序判断', '归档前处理', 50),
    ('gynecology', 'emr.patient.basic_info.ethnicity', TRUE, '程序判断', '归档前处理', 60),
    ('gynecology', 'emr.patient.basic_info.marital_status', TRUE, '程序判断', '归档前处理', 70),
    ('gynecology', 'emr.patient.basic_info.occupation', TRUE, '程序判断', '归档前处理', 80),
    ('gynecology', 'emr.patient.basic_info.encounter_time', TRUE, '程序判断', '归档前处理', 90),
    ('gynecology', 'emr.patient.basic_info.department', TRUE, '程序判断', '归档前处理', 100),
    ('gynecology', 'emr.patient.basic_info.attending_doctor', TRUE, '程序判断', '归档前处理', 110),
    ('gynecology', 'emr.patient.basic_info.pending_confirmation', TRUE, '程序判断', '归档前处理', 120),
    ('gynecology', 'emr.time.consistency.fields_present', TRUE, '程序判断', '归档后质控', 130),
    ('gynecology', 'emr.time.consistency.order', TRUE, '程序判断', '归档后质控', 140),
    ('gynecology', 'emr.time.consistency.last_modified', TRUE, '程序判断', '归档后质控', 150),
    ('gynecology', 'emr.time.consistency.abnormal_span', TRUE, '程序判断', '归档后质控', 160),
    ('gynecology', 'emr.chief_complaint.required', TRUE, '程序判断', '归档后质控', 170),
    ('gynecology', 'emr.chief_complaint.max_length', TRUE, '程序判断', '归档后质控', 180),
    ('gynecology', 'emr.chief_complaint.duration', TRUE, '大模型判断', '归档后质控', 190),
    ('gynecology', 'emr.chief_complaint.symptom_sign', TRUE, '大模型判断', '归档后质控', 200),
    ('gynecology', 'emr.chief_complaint.diagnosis_only', TRUE, '大模型判断', '归档后质控', 210),
    ('gynecology', 'emr.chief_complaint.not_vague', TRUE, '大模型判断', '归档后质控', 220),
    ('gynecology', 'emr.chief_complaint.consistency', TRUE, '大模型判断', '归档后质控', 230),
    ('gynecology', 'emr.present_illness.focus', TRUE, '大模型判断', '归档后质控', 240),
    ('gynecology', 'emr.present_illness.onset', TRUE, '大模型判断', '归档后质控', 250),
    ('gynecology', 'emr.present_illness.evolution', TRUE, '大模型判断', '归档后质控', 260),
    ('gynecology', 'emr.present_illness.associated_symptoms', TRUE, '大模型判断', '归档后质控', 270),
    ('gynecology', 'emr.present_illness.aggravating_relief', TRUE, '大模型判断', '归档后质控', 280),
    ('gynecology', 'emr.present_illness.prior_treatment', TRUE, '大模型判断', '归档后质控', 290),
    ('gynecology', 'emr.present_illness.treatment_effect', TRUE, '大模型判断', '归档后质控', 300),
    ('gynecology', 'emr.present_illness.followup_change', TRUE, '大模型判断', '归档后质控', 310),
    ('gynecology', 'emr.present_illness.general_status.diet', TRUE, '人工判断', '提交前处理', 320),
    ('gynecology', 'emr.present_illness.general_status.sleep', TRUE, '人工判断', '提交前处理', 330),
    ('gynecology', 'emr.present_illness.general_status.bowel', TRUE, '人工判断', '提交前处理', 340),
    ('gynecology', 'emr.present_illness.general_status.urination', TRUE, '人工判断', '提交前处理', 350),
    ('gynecology', 'emr.present_illness.general_status.weight_change', TRUE, '人工判断', '提交前处理', 360),
    ('gynecology', 'emr.past_history.chronic', TRUE, '大模型判断', '归档后质控', 370),
    ('gynecology', 'emr.past_history.surgery', TRUE, '人工判断', '归档前处理', 380),
    ('gynecology', 'emr.past_history.trauma', TRUE, '人工判断', '归档前处理', 390),
    ('gynecology', 'emr.past_history.infectious', TRUE, '人工判断', '归档前处理', 400),
    ('gynecology', 'emr.past_history.same_condition', TRUE, '人工判断', '归档前处理', 410),
    ('gynecology', 'emr.past_history.prior_treatment', TRUE, '大模型判断', '归档后质控', 420),
    ('gynecology', 'emr.personal_history.smoking', TRUE, '人工判断', '归档后质控', 430),
    ('gynecology', 'emr.personal_history.alcohol', TRUE, '人工判断', '归档后质控', 440),
    ('gynecology', 'emr.personal_history.exposure', TRUE, '人工判断', '归档后质控', 450),
    ('gynecology', 'emr.personal_history.lifestyle', TRUE, '人工判断', '归档后质控', 460),
    ('gynecology', 'emr.family_history.hereditary', TRUE, '人工判断', '归档后质控', 470),
    ('gynecology', 'emr.family_history.tumor', TRUE, '人工判断', '归档后质控', 480),
    ('gynecology', 'emr.family_history.chronic', TRUE, '人工判断', '归档后质控', 490),
    ('gynecology', 'emr.allergy.required.status', TRUE, '程序判断', '归档前处理', 500),
    ('gynecology', 'emr.allergy.required.none_evidence', TRUE, '人工判断', '归档前处理', 510),
    ('gynecology', 'emr.allergy.required.drug_detail', TRUE, '程序判断', '归档前处理', 520),
    ('gynecology', 'emr.allergy.required.other_detail', TRUE, '程序判断', '归档前处理', 530),
    ('gynecology', 'emr.allergy.required.unconfirmed', TRUE, '程序判断', '归档前处理', 540),
    ('gynecology', 'emr.allergy.prescription_risk.known_allergy', TRUE, '程序判断', '归档后质控', 550),
    ('gynecology', 'emr.allergy.prescription_risk.manual_confirmation', TRUE, '人工判断', '归档后质控', 560),
    ('gynecology', 'emr.physical_exam.vital_signs', TRUE, '程序判断', '归档后质控', 570),
    ('gynecology', 'emr.physical_exam.positive_signs', TRUE, '大模型判断', '归档后质控', 580),
    ('gynecology', 'emr.physical_exam.negative_signs', TRUE, '大模型判断', '归档后质控', 590),
    ('gynecology', 'emr.physical_exam.specialty', TRUE, '大模型判断', '归档后质控', 600),
    ('gynecology', 'emr.physical_exam.not_generic', TRUE, '大模型判断', '归档后质控', 610),
    ('gynecology', 'emr.auxiliary_exam.item_recorded', TRUE, '程序判断', '归档后质控', 620),
    ('gynecology', 'emr.auxiliary_exam.result_recorded', TRUE, '程序判断', '归档后质控', 630),
    ('gynecology', 'emr.auxiliary_exam.conclusion_recorded', TRUE, '人工判断', '归档后质控', 640),
    ('gynecology', 'emr.auxiliary_exam.pending_explicit', TRUE, '程序判断', '归档后质控', 650),
    ('gynecology', 'emr.auxiliary_exam.external_confirmed', TRUE, '人工判断', '归档后质控', 660),
    ('gynecology', 'emr.diagnosis.present', TRUE, '程序判断', '归档后质控', 670),
    ('gynecology', 'emr.diagnosis.standard_name', TRUE, '大模型判断', '归档后质控', 680),
    ('gynecology', 'emr.diagnosis.pending', TRUE, '大模型判断', '归档后质控', 690),
    ('gynecology', 'emr.diagnosis.followup_plan', TRUE, '程序判断', '归档后质控', 700),
    ('gynecology', 'emr.diagnosis.consistency.evidence_chain', TRUE, '大模型判断', '归档后质控', 710),
    ('gynecology', 'emr.diagnosis.consistency.history', TRUE, '大模型判断', '归档后质控', 720),
    ('gynecology', 'emr.diagnosis.consistency.exam', TRUE, '大模型判断', '归档后质控', 730),
    ('gynecology', 'emr.treatment.consistency.prescription', TRUE, '大模型判断', '归档后质控', 740),
    ('gynecology', 'emr.treatment.consistency.disposition', TRUE, '大模型判断', '归档后质控', 750),
    ('gynecology', 'emr.treatment.consistency.advice', TRUE, '大模型判断', '归档后质控', 760),
    ('gynecology', 'emr.treatment.consistency.recorded', TRUE, '程序判断', '归档后质控', 770),
    ('gynecology', 'emr.treatment.consistency.followup', TRUE, '人工判断', '归档后质控', 780),
    ('gynecology', 'emr.followup.time', TRUE, '人工判断', '归档前处理', 790),
    ('gynecology', 'emr.followup.trigger', TRUE, '人工判断', '归档前处理', 800),
    ('gynecology', 'emr.followup.requirement', TRUE, '人工判断', '归档前处理', 810),
    ('gynecology', 'emr.signature.doctor', TRUE, '程序判断', '归档前处理', 820),
    ('gynecology', 'emr.signature.supervisor', TRUE, '程序判断', '归档前处理', 830),
    ('gynecology', 'emr.audit.trail.operator', TRUE, '程序判断', '归档后质控', 840),
    ('gynecology', 'emr.audit.trail.time', TRUE, '程序判断', '归档后质控', 850),
    ('gynecology', 'emr.audit.trail.reason', TRUE, '程序判断', '归档后质控', 860),
    ('gynecology', 'emr.audit.trail.before_value', TRUE, '程序判断', '归档后质控', 870),
    ('gynecology', 'emr.audit.trail.after_value', TRUE, '程序判断', '归档后质控', 880),
    ('gynecology', 'emr.gynecology.menstrual_history.cycle', TRUE, '人工判断', '归档前处理', 890),
    ('gynecology', 'emr.gynecology.menstrual_history.duration', TRUE, '人工判断', '归档前处理', 900),
    ('gynecology', 'emr.gynecology.menstrual_history.amount', TRUE, '人工判断', '归档前处理', 910),
    ('gynecology', 'emr.gynecology.menstrual_history.dysmenorrhea', TRUE, '人工判断', '归档前处理', 920),
    ('gynecology', 'emr.gynecology.menstrual_history.lmp', TRUE, '人工判断', '归档前处理', 930),
    ('gynecology', 'emr.gynecology.menstrual_history.applicability', TRUE, '程序判断', '归档前处理', 940),
    ('gynecology', 'emr.gynecology.marriage_childbearing.marital', TRUE, '人工判断', '归档前处理', 950),
    ('gynecology', 'emr.gynecology.marriage_childbearing.gravidity', TRUE, '人工判断', '归档前处理', 960),
    ('gynecology', 'emr.gynecology.marriage_childbearing.parity', TRUE, '人工判断', '归档前处理', 970),
    ('gynecology', 'emr.gynecology.marriage_childbearing.abortion', TRUE, '人工判断', '归档前处理', 980),
    ('gynecology', 'emr.gynecology.marriage_childbearing.contraception', TRUE, '人工判断', '归档前处理', 990),
    ('gynecology', 'emr.gynecology.exam_required.exam_or_reason', TRUE, '人工判断', '归档后质控', 1000),
    ('gynecology', 'common.visible_fields.chief_complaint', TRUE, '程序判断', '模板发布前处理', 1010),
    ('gynecology', 'common.visible_fields.present_illness', TRUE, '程序判断', '模板发布前处理', 1020),
    ('gynecology', 'common.visible_fields.past_history', TRUE, '程序判断', '模板发布前处理', 1030),
    ('gynecology', 'common.visible_fields.personal_history', TRUE, '程序判断', '模板发布前处理', 1040),
    ('gynecology', 'common.visible_fields.family_history', TRUE, '程序判断', '模板发布前处理', 1050),
    ('gynecology', 'common.visible_fields.allergy_history', TRUE, '程序判断', '模板发布前处理', 1060),
    ('gynecology', 'common.visible_fields.physical_exam', TRUE, '程序判断', '模板发布前处理', 1070),
    ('gynecology', 'common.visible_fields.auxiliary_exam', TRUE, '程序判断', '模板发布前处理', 1080),
    ('gynecology', 'common.visible_fields.diagnosis', TRUE, '程序判断', '模板发布前处理', 1090),
    ('gynecology', 'common.visible_fields.prescription', TRUE, '程序判断', '模板发布前处理', 1100),
    ('gynecology', 'common.visible_fields.disposition', TRUE, '程序判断', '模板发布前处理', 1110),
    ('gynecology', 'common.visible_fields.medical_advice', TRUE, '程序判断', '模板发布前处理', 1120),
    ('gynecology', 'common.visible_fields.followup', TRUE, '程序判断', '模板发布前处理', 1130),
    ('gynecology', 'common.visible_fields.supplement', TRUE, '程序判断', '模板发布前处理', 1140),
    ('gynecology', 'common.orders_split.prescription', TRUE, '程序判断', '模板发布前处理', 1150),
    ('gynecology', 'common.orders_split.treatment', TRUE, '程序判断', '模板发布前处理', 1160),
    ('gynecology', 'common.orders_split.exam', TRUE, '程序判断', '模板发布前处理', 1170),
    ('gynecology', 'common.orders_split.infusion', TRUE, '程序判断', '模板发布前处理', 1180),
    ('gynecology', 'common.orders_split.procedure', TRUE, '程序判断', '模板发布前处理', 1190),
    ('gynecology', 'gynecology.history.menstrual_field', TRUE, '程序判断', '模板发布前处理', 1200),
    ('gynecology', 'gynecology.history.marriage_field', TRUE, '程序判断', '模板发布前处理', 1210),
    ('gynecology', 'gynecology.history.pregnancy_field', TRUE, '程序判断', '模板发布前处理', 1220),
    ('gynecology', 'gynecology.history.exam_field', TRUE, '程序判断', '模板发布前处理', 1230),
    ('gynecology', 'gynecology.exam.physical_group', TRUE, '程序判断', '模板发布前处理', 1240),
    ('gynecology', 'gynecology.exam.not_parallel', TRUE, '程序判断', '模板发布前处理', 1250),
    ('pediatrics', 'emr.patient.basic_info.name', TRUE, '程序判断', '归档前处理', 10),
    ('pediatrics', 'emr.patient.basic_info.gender', TRUE, '程序判断', '归档前处理', 20),
    ('pediatrics', 'emr.patient.basic_info.birth_or_age', TRUE, '程序判断', '归档前处理', 30),
    ('pediatrics', 'emr.patient.basic_info.phone', TRUE, '程序判断', '归档前处理', 40),
    ('pediatrics', 'emr.patient.basic_info.address', TRUE, '程序判断', '归档前处理', 50),
    ('pediatrics', 'emr.patient.basic_info.ethnicity', TRUE, '程序判断', '归档前处理', 60),
    ('pediatrics', 'emr.patient.basic_info.marital_status', TRUE, '程序判断', '归档前处理', 70),
    ('pediatrics', 'emr.patient.basic_info.occupation', TRUE, '程序判断', '归档前处理', 80),
    ('pediatrics', 'emr.patient.basic_info.encounter_time', TRUE, '程序判断', '归档前处理', 90),
    ('pediatrics', 'emr.patient.basic_info.department', TRUE, '程序判断', '归档前处理', 100),
    ('pediatrics', 'emr.patient.basic_info.attending_doctor', TRUE, '程序判断', '归档前处理', 110),
    ('pediatrics', 'emr.patient.basic_info.pending_confirmation', TRUE, '程序判断', '归档前处理', 120),
    ('pediatrics', 'emr.time.consistency.fields_present', TRUE, '程序判断', '归档后质控', 130),
    ('pediatrics', 'emr.time.consistency.order', TRUE, '程序判断', '归档后质控', 140),
    ('pediatrics', 'emr.time.consistency.last_modified', TRUE, '程序判断', '归档后质控', 150),
    ('pediatrics', 'emr.time.consistency.abnormal_span', TRUE, '程序判断', '归档后质控', 160),
    ('pediatrics', 'emr.chief_complaint.required', TRUE, '程序判断', '归档后质控', 170),
    ('pediatrics', 'emr.chief_complaint.max_length', TRUE, '程序判断', '归档后质控', 180),
    ('pediatrics', 'emr.chief_complaint.duration', TRUE, '大模型判断', '归档后质控', 190),
    ('pediatrics', 'emr.chief_complaint.symptom_sign', TRUE, '大模型判断', '归档后质控', 200),
    ('pediatrics', 'emr.chief_complaint.diagnosis_only', TRUE, '大模型判断', '归档后质控', 210),
    ('pediatrics', 'emr.chief_complaint.not_vague', TRUE, '大模型判断', '归档后质控', 220),
    ('pediatrics', 'emr.chief_complaint.consistency', TRUE, '大模型判断', '归档后质控', 230),
    ('pediatrics', 'emr.present_illness.focus', TRUE, '大模型判断', '归档后质控', 240),
    ('pediatrics', 'emr.present_illness.onset', TRUE, '大模型判断', '归档后质控', 250),
    ('pediatrics', 'emr.present_illness.evolution', TRUE, '大模型判断', '归档后质控', 260),
    ('pediatrics', 'emr.present_illness.associated_symptoms', TRUE, '大模型判断', '归档后质控', 270),
    ('pediatrics', 'emr.present_illness.aggravating_relief', TRUE, '大模型判断', '归档后质控', 280),
    ('pediatrics', 'emr.present_illness.prior_treatment', TRUE, '大模型判断', '归档后质控', 290),
    ('pediatrics', 'emr.present_illness.treatment_effect', TRUE, '大模型判断', '归档后质控', 300),
    ('pediatrics', 'emr.present_illness.followup_change', TRUE, '大模型判断', '归档后质控', 310),
    ('pediatrics', 'emr.present_illness.general_status.diet', TRUE, '人工判断', '提交前处理', 320),
    ('pediatrics', 'emr.present_illness.general_status.sleep', TRUE, '人工判断', '提交前处理', 330),
    ('pediatrics', 'emr.present_illness.general_status.bowel', TRUE, '人工判断', '提交前处理', 340),
    ('pediatrics', 'emr.present_illness.general_status.urination', TRUE, '人工判断', '提交前处理', 350),
    ('pediatrics', 'emr.present_illness.general_status.weight_change', TRUE, '人工判断', '提交前处理', 360),
    ('pediatrics', 'emr.past_history.chronic', TRUE, '大模型判断', '归档后质控', 370),
    ('pediatrics', 'emr.past_history.surgery', TRUE, '人工判断', '归档前处理', 380),
    ('pediatrics', 'emr.past_history.trauma', TRUE, '人工判断', '归档前处理', 390),
    ('pediatrics', 'emr.past_history.infectious', TRUE, '人工判断', '归档前处理', 400),
    ('pediatrics', 'emr.past_history.same_condition', TRUE, '人工判断', '归档前处理', 410),
    ('pediatrics', 'emr.past_history.prior_treatment', TRUE, '大模型判断', '归档后质控', 420),
    ('pediatrics', 'emr.personal_history.smoking', TRUE, '人工判断', '归档后质控', 430),
    ('pediatrics', 'emr.personal_history.alcohol', TRUE, '人工判断', '归档后质控', 440),
    ('pediatrics', 'emr.personal_history.exposure', TRUE, '人工判断', '归档后质控', 450),
    ('pediatrics', 'emr.personal_history.lifestyle', TRUE, '人工判断', '归档后质控', 460),
    ('pediatrics', 'emr.family_history.hereditary', TRUE, '人工判断', '归档后质控', 470),
    ('pediatrics', 'emr.family_history.tumor', TRUE, '人工判断', '归档后质控', 480),
    ('pediatrics', 'emr.family_history.chronic', TRUE, '人工判断', '归档后质控', 490),
    ('pediatrics', 'emr.allergy.required.status', TRUE, '程序判断', '归档前处理', 500),
    ('pediatrics', 'emr.allergy.required.none_evidence', TRUE, '人工判断', '归档前处理', 510),
    ('pediatrics', 'emr.allergy.required.drug_detail', TRUE, '程序判断', '归档前处理', 520),
    ('pediatrics', 'emr.allergy.required.other_detail', TRUE, '程序判断', '归档前处理', 530),
    ('pediatrics', 'emr.allergy.required.unconfirmed', TRUE, '程序判断', '归档前处理', 540),
    ('pediatrics', 'emr.allergy.prescription_risk.known_allergy', TRUE, '程序判断', '归档后质控', 550),
    ('pediatrics', 'emr.allergy.prescription_risk.manual_confirmation', TRUE, '人工判断', '归档后质控', 560),
    ('pediatrics', 'emr.physical_exam.vital_signs', TRUE, '程序判断', '归档后质控', 570),
    ('pediatrics', 'emr.physical_exam.positive_signs', TRUE, '大模型判断', '归档后质控', 580),
    ('pediatrics', 'emr.physical_exam.negative_signs', TRUE, '大模型判断', '归档后质控', 590),
    ('pediatrics', 'emr.physical_exam.specialty', TRUE, '大模型判断', '归档后质控', 600),
    ('pediatrics', 'emr.physical_exam.not_generic', TRUE, '大模型判断', '归档后质控', 610),
    ('pediatrics', 'emr.auxiliary_exam.item_recorded', TRUE, '程序判断', '归档后质控', 620),
    ('pediatrics', 'emr.auxiliary_exam.result_recorded', TRUE, '程序判断', '归档后质控', 630),
    ('pediatrics', 'emr.auxiliary_exam.conclusion_recorded', TRUE, '人工判断', '归档后质控', 640),
    ('pediatrics', 'emr.auxiliary_exam.pending_explicit', TRUE, '程序判断', '归档后质控', 650),
    ('pediatrics', 'emr.auxiliary_exam.external_confirmed', TRUE, '人工判断', '归档后质控', 660),
    ('pediatrics', 'emr.diagnosis.present', TRUE, '程序判断', '归档后质控', 670),
    ('pediatrics', 'emr.diagnosis.standard_name', TRUE, '大模型判断', '归档后质控', 680),
    ('pediatrics', 'emr.diagnosis.pending', TRUE, '大模型判断', '归档后质控', 690),
    ('pediatrics', 'emr.diagnosis.followup_plan', TRUE, '程序判断', '归档后质控', 700),
    ('pediatrics', 'emr.diagnosis.consistency.evidence_chain', TRUE, '大模型判断', '归档后质控', 710),
    ('pediatrics', 'emr.diagnosis.consistency.history', TRUE, '大模型判断', '归档后质控', 720),
    ('pediatrics', 'emr.diagnosis.consistency.exam', TRUE, '大模型判断', '归档后质控', 730),
    ('pediatrics', 'emr.treatment.consistency.prescription', TRUE, '大模型判断', '归档后质控', 740),
    ('pediatrics', 'emr.treatment.consistency.disposition', TRUE, '大模型判断', '归档后质控', 750),
    ('pediatrics', 'emr.treatment.consistency.advice', TRUE, '大模型判断', '归档后质控', 760),
    ('pediatrics', 'emr.treatment.consistency.recorded', TRUE, '程序判断', '归档后质控', 770),
    ('pediatrics', 'emr.treatment.consistency.followup', TRUE, '人工判断', '归档后质控', 780),
    ('pediatrics', 'emr.followup.time', TRUE, '人工判断', '归档前处理', 790),
    ('pediatrics', 'emr.followup.trigger', TRUE, '人工判断', '归档前处理', 800),
    ('pediatrics', 'emr.followup.requirement', TRUE, '人工判断', '归档前处理', 810),
    ('pediatrics', 'emr.signature.doctor', TRUE, '程序判断', '归档前处理', 820),
    ('pediatrics', 'emr.signature.supervisor', TRUE, '程序判断', '归档前处理', 830),
    ('pediatrics', 'emr.audit.trail.operator', TRUE, '程序判断', '归档后质控', 840),
    ('pediatrics', 'emr.audit.trail.time', TRUE, '程序判断', '归档后质控', 850),
    ('pediatrics', 'emr.audit.trail.reason', TRUE, '程序判断', '归档后质控', 860),
    ('pediatrics', 'emr.audit.trail.before_value', TRUE, '程序判断', '归档后质控', 870),
    ('pediatrics', 'emr.audit.trail.after_value', TRUE, '程序判断', '归档后质控', 880),
    ('pediatrics', 'emr.pediatrics.guardian_info.name', TRUE, '程序判断', '归档前处理', 890),
    ('pediatrics', 'emr.pediatrics.guardian_info.relationship', TRUE, '程序判断', '归档前处理', 900),
    ('pediatrics', 'emr.pediatrics.guardian_info.contact', TRUE, '程序判断', '归档前处理', 910),
    ('pediatrics', 'emr.pediatrics.fever_peak.temperature', TRUE, '程序判断', '归档前处理', 920),
    ('pediatrics', 'emr.pediatrics.fever_peak.duration', TRUE, '程序判断', '归档前处理', 930),
    ('pediatrics', 'emr.pediatrics.fever_peak.antipyretic', TRUE, '人工判断', '归档前处理', 940),
    ('pediatrics', 'emr.pediatrics.fever_peak.mental_status', TRUE, '人工判断', '归档前处理', 950),
    ('pediatrics', 'emr.pediatrics.weight_dose.age', TRUE, '程序判断', '归档后质控', 960),
    ('pediatrics', 'emr.pediatrics.weight_dose.weight_basis', TRUE, '程序判断', '归档后质控', 970),
    ('pediatrics', 'emr.pediatrics.weight_dose.dose', TRUE, '程序判断', '归档后质控', 980),
    ('pediatrics', 'emr.pediatrics.weight_dose.route', TRUE, '程序判断', '归档后质控', 990),
    ('pediatrics', 'emr.pediatrics.weight_dose.course', TRUE, '程序判断', '归档后质控', 1000),
    ('pediatrics', 'emr.pediatrics.weight_dose.abnormal', TRUE, '人工判断', '归档后质控', 1010),
    ('pediatrics', 'emr.pediatrics.feeding_growth.feeding', TRUE, '人工判断', '归档后质控', 1020),
    ('pediatrics', 'emr.pediatrics.feeding_growth.growth', TRUE, '人工判断', '归档后质控', 1030),
    ('pediatrics', 'emr.pediatrics.feeding_growth.vaccination', TRUE, '人工判断', '归档后质控', 1040),
    ('pediatrics', 'common.visible_fields.chief_complaint', TRUE, '程序判断', '模板发布前处理', 1050),
    ('pediatrics', 'common.visible_fields.present_illness', TRUE, '程序判断', '模板发布前处理', 1060),
    ('pediatrics', 'common.visible_fields.past_history', TRUE, '程序判断', '模板发布前处理', 1070),
    ('pediatrics', 'common.visible_fields.personal_history', TRUE, '程序判断', '模板发布前处理', 1080),
    ('pediatrics', 'common.visible_fields.family_history', TRUE, '程序判断', '模板发布前处理', 1090),
    ('pediatrics', 'common.visible_fields.allergy_history', TRUE, '程序判断', '模板发布前处理', 1100),
    ('pediatrics', 'common.visible_fields.physical_exam', TRUE, '程序判断', '模板发布前处理', 1110),
    ('pediatrics', 'common.visible_fields.auxiliary_exam', TRUE, '程序判断', '模板发布前处理', 1120),
    ('pediatrics', 'common.visible_fields.diagnosis', TRUE, '程序判断', '模板发布前处理', 1130),
    ('pediatrics', 'common.visible_fields.prescription', TRUE, '程序判断', '模板发布前处理', 1140),
    ('pediatrics', 'common.visible_fields.disposition', TRUE, '程序判断', '模板发布前处理', 1150),
    ('pediatrics', 'common.visible_fields.medical_advice', TRUE, '程序判断', '模板发布前处理', 1160),
    ('pediatrics', 'common.visible_fields.followup', TRUE, '程序判断', '模板发布前处理', 1170),
    ('pediatrics', 'common.visible_fields.supplement', TRUE, '程序判断', '模板发布前处理', 1180),
    ('pediatrics', 'common.orders_split.prescription', TRUE, '程序判断', '模板发布前处理', 1190),
    ('pediatrics', 'common.orders_split.treatment', TRUE, '程序判断', '模板发布前处理', 1200),
    ('pediatrics', 'common.orders_split.exam', TRUE, '程序判断', '模板发布前处理', 1210),
    ('pediatrics', 'common.orders_split.infusion', TRUE, '程序判断', '模板发布前处理', 1220),
    ('pediatrics', 'common.orders_split.procedure', TRUE, '程序判断', '模板发布前处理', 1230),
    ('pediatrics', 'pediatrics.guardian_block.guardian', TRUE, '程序判断', '模板发布前处理', 1240),
    ('pediatrics', 'pediatrics.guardian_block.feeding', TRUE, '程序判断', '模板发布前处理', 1250),
    ('pediatrics', 'pediatrics.guardian_block.vaccination', TRUE, '程序判断', '模板发布前处理', 1260),
    ('pediatrics', 'pediatrics.guardian_block.growth', TRUE, '程序判断', '模板发布前处理', 1270),
    ('dental', 'emr.patient.basic_info.name', TRUE, '程序判断', '归档前处理', 10),
    ('dental', 'emr.patient.basic_info.gender', TRUE, '程序判断', '归档前处理', 20),
    ('dental', 'emr.patient.basic_info.birth_or_age', TRUE, '程序判断', '归档前处理', 30),
    ('dental', 'emr.patient.basic_info.phone', TRUE, '程序判断', '归档前处理', 40),
    ('dental', 'emr.patient.basic_info.address', TRUE, '程序判断', '归档前处理', 50),
    ('dental', 'emr.patient.basic_info.ethnicity', TRUE, '程序判断', '归档前处理', 60),
    ('dental', 'emr.patient.basic_info.marital_status', TRUE, '程序判断', '归档前处理', 70),
    ('dental', 'emr.patient.basic_info.occupation', TRUE, '程序判断', '归档前处理', 80),
    ('dental', 'emr.patient.basic_info.encounter_time', TRUE, '程序判断', '归档前处理', 90),
    ('dental', 'emr.patient.basic_info.department', TRUE, '程序判断', '归档前处理', 100),
    ('dental', 'emr.patient.basic_info.attending_doctor', TRUE, '程序判断', '归档前处理', 110),
    ('dental', 'emr.patient.basic_info.pending_confirmation', TRUE, '程序判断', '归档前处理', 120),
    ('dental', 'emr.time.consistency.fields_present', TRUE, '程序判断', '归档后质控', 130),
    ('dental', 'emr.time.consistency.order', TRUE, '程序判断', '归档后质控', 140),
    ('dental', 'emr.time.consistency.last_modified', TRUE, '程序判断', '归档后质控', 150),
    ('dental', 'emr.time.consistency.abnormal_span', TRUE, '程序判断', '归档后质控', 160),
    ('dental', 'emr.chief_complaint.required', TRUE, '程序判断', '归档后质控', 170),
    ('dental', 'emr.chief_complaint.max_length', TRUE, '程序判断', '归档后质控', 180),
    ('dental', 'emr.chief_complaint.duration', TRUE, '大模型判断', '归档后质控', 190),
    ('dental', 'emr.chief_complaint.symptom_sign', TRUE, '大模型判断', '归档后质控', 200),
    ('dental', 'emr.chief_complaint.diagnosis_only', TRUE, '大模型判断', '归档后质控', 210),
    ('dental', 'emr.chief_complaint.not_vague', TRUE, '大模型判断', '归档后质控', 220),
    ('dental', 'emr.chief_complaint.consistency', TRUE, '大模型判断', '归档后质控', 230),
    ('dental', 'emr.present_illness.focus', TRUE, '大模型判断', '归档后质控', 240),
    ('dental', 'emr.present_illness.onset', TRUE, '大模型判断', '归档后质控', 250),
    ('dental', 'emr.present_illness.evolution', TRUE, '大模型判断', '归档后质控', 260),
    ('dental', 'emr.present_illness.associated_symptoms', TRUE, '大模型判断', '归档后质控', 270),
    ('dental', 'emr.present_illness.aggravating_relief', TRUE, '大模型判断', '归档后质控', 280),
    ('dental', 'emr.present_illness.prior_treatment', TRUE, '大模型判断', '归档后质控', 290),
    ('dental', 'emr.present_illness.treatment_effect', TRUE, '大模型判断', '归档后质控', 300),
    ('dental', 'emr.present_illness.followup_change', TRUE, '大模型判断', '归档后质控', 310),
    ('dental', 'emr.present_illness.general_status.diet', TRUE, '人工判断', '提交前处理', 320),
    ('dental', 'emr.present_illness.general_status.sleep', TRUE, '人工判断', '提交前处理', 330),
    ('dental', 'emr.present_illness.general_status.bowel', TRUE, '人工判断', '提交前处理', 340),
    ('dental', 'emr.present_illness.general_status.urination', TRUE, '人工判断', '提交前处理', 350),
    ('dental', 'emr.present_illness.general_status.weight_change', TRUE, '人工判断', '提交前处理', 360),
    ('dental', 'emr.past_history.chronic', TRUE, '大模型判断', '归档后质控', 370),
    ('dental', 'emr.past_history.surgery', TRUE, '人工判断', '归档前处理', 380),
    ('dental', 'emr.past_history.trauma', TRUE, '人工判断', '归档前处理', 390),
    ('dental', 'emr.past_history.infectious', TRUE, '人工判断', '归档前处理', 400),
    ('dental', 'emr.past_history.same_condition', TRUE, '人工判断', '归档前处理', 410),
    ('dental', 'emr.past_history.prior_treatment', TRUE, '大模型判断', '归档后质控', 420),
    ('dental', 'emr.personal_history.smoking', TRUE, '人工判断', '归档后质控', 430),
    ('dental', 'emr.personal_history.alcohol', TRUE, '人工判断', '归档后质控', 440),
    ('dental', 'emr.personal_history.exposure', TRUE, '人工判断', '归档后质控', 450),
    ('dental', 'emr.personal_history.lifestyle', TRUE, '人工判断', '归档后质控', 460),
    ('dental', 'emr.family_history.hereditary', TRUE, '人工判断', '归档后质控', 470),
    ('dental', 'emr.family_history.tumor', TRUE, '人工判断', '归档后质控', 480),
    ('dental', 'emr.family_history.chronic', TRUE, '人工判断', '归档后质控', 490),
    ('dental', 'emr.allergy.required.status', TRUE, '程序判断', '归档前处理', 500),
    ('dental', 'emr.allergy.required.none_evidence', TRUE, '人工判断', '归档前处理', 510),
    ('dental', 'emr.allergy.required.drug_detail', TRUE, '程序判断', '归档前处理', 520),
    ('dental', 'emr.allergy.required.other_detail', TRUE, '程序判断', '归档前处理', 530),
    ('dental', 'emr.allergy.required.unconfirmed', TRUE, '程序判断', '归档前处理', 540),
    ('dental', 'emr.allergy.prescription_risk.known_allergy', TRUE, '程序判断', '归档后质控', 550),
    ('dental', 'emr.allergy.prescription_risk.manual_confirmation', TRUE, '人工判断', '归档后质控', 560),
    ('dental', 'emr.physical_exam.vital_signs', TRUE, '程序判断', '归档后质控', 570),
    ('dental', 'emr.physical_exam.positive_signs', TRUE, '大模型判断', '归档后质控', 580),
    ('dental', 'emr.physical_exam.negative_signs', TRUE, '大模型判断', '归档后质控', 590),
    ('dental', 'emr.physical_exam.specialty', TRUE, '大模型判断', '归档后质控', 600),
    ('dental', 'emr.physical_exam.not_generic', TRUE, '大模型判断', '归档后质控', 610),
    ('dental', 'emr.auxiliary_exam.item_recorded', TRUE, '程序判断', '归档后质控', 620),
    ('dental', 'emr.auxiliary_exam.result_recorded', TRUE, '程序判断', '归档后质控', 630),
    ('dental', 'emr.auxiliary_exam.conclusion_recorded', TRUE, '人工判断', '归档后质控', 640),
    ('dental', 'emr.auxiliary_exam.pending_explicit', TRUE, '程序判断', '归档后质控', 650),
    ('dental', 'emr.auxiliary_exam.external_confirmed', TRUE, '人工判断', '归档后质控', 660),
    ('dental', 'emr.diagnosis.present', TRUE, '程序判断', '归档后质控', 670),
    ('dental', 'emr.diagnosis.standard_name', TRUE, '大模型判断', '归档后质控', 680),
    ('dental', 'emr.diagnosis.pending', TRUE, '大模型判断', '归档后质控', 690),
    ('dental', 'emr.diagnosis.followup_plan', TRUE, '程序判断', '归档后质控', 700),
    ('dental', 'emr.diagnosis.consistency.evidence_chain', TRUE, '大模型判断', '归档后质控', 710),
    ('dental', 'emr.diagnosis.consistency.history', TRUE, '大模型判断', '归档后质控', 720),
    ('dental', 'emr.diagnosis.consistency.exam', TRUE, '大模型判断', '归档后质控', 730),
    ('dental', 'emr.treatment.consistency.prescription', TRUE, '大模型判断', '归档后质控', 740),
    ('dental', 'emr.treatment.consistency.disposition', TRUE, '大模型判断', '归档后质控', 750),
    ('dental', 'emr.treatment.consistency.advice', TRUE, '大模型判断', '归档后质控', 760),
    ('dental', 'emr.treatment.consistency.recorded', TRUE, '程序判断', '归档后质控', 770),
    ('dental', 'emr.treatment.consistency.followup', TRUE, '人工判断', '归档后质控', 780),
    ('dental', 'emr.followup.time', TRUE, '人工判断', '归档前处理', 790),
    ('dental', 'emr.followup.trigger', TRUE, '人工判断', '归档前处理', 800),
    ('dental', 'emr.followup.requirement', TRUE, '人工判断', '归档前处理', 810),
    ('dental', 'emr.signature.doctor', TRUE, '程序判断', '归档前处理', 820),
    ('dental', 'emr.signature.supervisor', TRUE, '程序判断', '归档前处理', 830),
    ('dental', 'emr.audit.trail.operator', TRUE, '程序判断', '归档后质控', 840),
    ('dental', 'emr.audit.trail.time', TRUE, '程序判断', '归档后质控', 850),
    ('dental', 'emr.audit.trail.reason', TRUE, '程序判断', '归档后质控', 860),
    ('dental', 'emr.audit.trail.before_value', TRUE, '程序判断', '归档后质控', 870),
    ('dental', 'emr.audit.trail.after_value', TRUE, '程序判断', '归档后质控', 880),
    ('dental', 'emr.dental.tooth_plan.tooth_present', TRUE, '程序判断', '归档后质控', 890),
    ('dental', 'emr.dental.tooth_plan.exam_match', TRUE, '人工判断', '归档后质控', 900),
    ('dental', 'emr.dental.tooth_plan.imaging_match', TRUE, '人工判断', '归档后质控', 910),
    ('dental', 'emr.dental.tooth_plan.plan_match', TRUE, '人工判断', '归档后质控', 920),
    ('dental', 'emr.dental.informed_consent.notice', TRUE, '程序判断', '归档后质控', 930),
    ('dental', 'emr.dental.informed_consent.agreement', TRUE, '程序判断', '归档后质控', 940),
    ('dental', 'emr.dental.informed_consent.procedure', TRUE, '人工判断', '归档后质控', 950),
    ('dental', 'common.visible_fields.chief_complaint', TRUE, '程序判断', '模板发布前处理', 960),
    ('dental', 'common.visible_fields.present_illness', TRUE, '程序判断', '模板发布前处理', 970),
    ('dental', 'common.visible_fields.past_history', TRUE, '程序判断', '模板发布前处理', 980),
    ('dental', 'common.visible_fields.personal_history', TRUE, '程序判断', '模板发布前处理', 990),
    ('dental', 'common.visible_fields.family_history', TRUE, '程序判断', '模板发布前处理', 1000),
    ('dental', 'common.visible_fields.allergy_history', TRUE, '程序判断', '模板发布前处理', 1010),
    ('dental', 'common.visible_fields.physical_exam', TRUE, '程序判断', '模板发布前处理', 1020),
    ('dental', 'common.visible_fields.auxiliary_exam', TRUE, '程序判断', '模板发布前处理', 1030),
    ('dental', 'common.visible_fields.diagnosis', TRUE, '程序判断', '模板发布前处理', 1040),
    ('dental', 'common.visible_fields.prescription', TRUE, '程序判断', '模板发布前处理', 1050),
    ('dental', 'common.visible_fields.disposition', TRUE, '程序判断', '模板发布前处理', 1060),
    ('dental', 'common.visible_fields.medical_advice', TRUE, '程序判断', '模板发布前处理', 1070),
    ('dental', 'common.visible_fields.followup', TRUE, '程序判断', '模板发布前处理', 1080),
    ('dental', 'common.visible_fields.supplement', TRUE, '程序判断', '模板发布前处理', 1090),
    ('dental', 'common.orders_split.prescription', TRUE, '程序判断', '模板发布前处理', 1100),
    ('dental', 'common.orders_split.treatment', TRUE, '程序判断', '模板发布前处理', 1110),
    ('dental', 'common.orders_split.exam', TRUE, '程序判断', '模板发布前处理', 1120),
    ('dental', 'common.orders_split.infusion', TRUE, '程序判断', '模板发布前处理', 1130),
    ('dental', 'common.orders_split.procedure', TRUE, '程序判断', '模板发布前处理', 1140),
    ('dental', 'dental.tooth_position.field', TRUE, '程序判断', '模板发布前处理', 1150),
    ('dental', 'dental.tooth_position.diagnosis_link', TRUE, '程序判断', '模板发布前处理', 1160),
    ('dental', 'dental.tooth_position.imaging_link', TRUE, '程序判断', '模板发布前处理', 1170),
    ('dental', 'dental.tooth_position.treatment_link', TRUE, '程序判断', '模板发布前处理', 1180),
    ('dental', 'dental.exam.physical_group', TRUE, '程序判断', '模板发布前处理', 1190),
    ('dental', 'dental.exam.not_parallel', TRUE, '程序判断', '模板发布前处理', 1200);

DO $$
DECLARE
    v_template_code TEXT;
    v_quality_rule_code TEXT;
    template_id UUID;
    quality_rule_id UUID;
    expected_count INTEGER;
    actual_count INTEGER;
    seed_count INTEGER;
    distinct_seed_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO seed_count
    FROM _emr_seed_template_quality_rules;
    IF seed_count <> 609 THEN
        RAISE EXCEPTION 'EMR 模板质量规则种子数量不正确：expected=609, actual=%', seed_count;
    END IF;

    SELECT COUNT(*) INTO distinct_seed_count
    FROM (
        SELECT DISTINCT template_code, quality_rule_code
        FROM _emr_seed_template_quality_rules
    ) AS unique_items;
    IF distinct_seed_count <> seed_count THEN
        RAISE EXCEPTION 'EMR 模板质量规则种子存在重复绑定：total=%, distinct=%', seed_count, distinct_seed_count;
    END IF;

    FOREACH v_template_code IN ARRAY ARRAY[
        'internal_medicine', 'tcm', 'gynecology', 'pediatrics', 'dental'
    ] LOOP
        SELECT t.id
        INTO template_id
        FROM public.emr_templates t
        WHERE t.tenant_id = 0
          AND t.code = v_template_code
          AND t.status = 'published'
          AND t.is_current = TRUE;

        IF template_id IS NULL THEN
            RAISE EXCEPTION
                'EMR 平台默认模板不存在可用当前版本：code=%（要求 tenant_id=0、status=published、is_current=true）',
                v_template_code;
        END IF;

        SELECT COUNT(*)
        INTO actual_count
        FROM public.emr_templates t
        WHERE t.tenant_id = 0
          AND t.code = v_template_code
          AND t.status = 'published'
          AND t.is_current = TRUE;
        IF actual_count <> 1 THEN
            RAISE EXCEPTION 'EMR 平台默认模板当前版本不唯一：code=%, count=%', v_template_code, actual_count;
        END IF;
    END LOOP;

    FOR v_quality_rule_code IN
        SELECT DISTINCT s.quality_rule_code
        FROM _emr_seed_template_quality_rules s
        ORDER BY s.quality_rule_code
    LOOP
        SELECT q.id
        INTO quality_rule_id
        FROM public.emr_quality_rules q
        WHERE q.tenant_id = 0
          AND q.code = v_quality_rule_code
          AND q.status = 'published';

        IF quality_rule_id IS NULL THEN
            RAISE EXCEPTION
                'EMR 质量规则不存在或未发布：code=%（要求 tenant_id=0、status=published）',
                v_quality_rule_code;
        END IF;
    END LOOP;

    -- 平台默认绑定以种子文件为准。只清理这五套模板的 tenant_id=0 绑定，
    -- 不触碰任何租户自定义绑定，也不依赖历史唯一索引。
    DELETE FROM public.emr_template_quality_rules b
    USING public.emr_templates t
    WHERE b.tenant_id = 0
      AND b.template_id = t.id
      AND t.tenant_id = 0
      AND t.code IN ('internal_medicine', 'tcm', 'gynecology', 'pediatrics', 'dental');

    INSERT INTO public.emr_template_quality_rules
        (tenant_id, template_id, quality_rule_id, is_enabled, execution_mode,
         deadline_action, display_order, created_by)
    SELECT
        0,
        t.id,
        q.id,
        s.is_enabled,
        s.execution_mode,
        s.deadline_action,
        s.display_order,
        0
    FROM _emr_seed_template_quality_rules s
    JOIN public.emr_templates t
      ON t.tenant_id = 0
     AND t.code = s.template_code
     AND t.status = 'published'
     AND t.is_current = TRUE
    JOIN public.emr_quality_rules q
      ON q.tenant_id = 0
     AND q.code = s.quality_rule_code
     AND q.status = 'published';

    SELECT COUNT(*)
    INTO actual_count
    FROM public.emr_template_quality_rules b
    JOIN public.emr_templates t ON t.id = b.template_id
    WHERE b.tenant_id = 0
      AND t.tenant_id = 0
      AND t.code IN ('internal_medicine', 'tcm', 'gynecology', 'pediatrics', 'dental');
    IF actual_count <> seed_count THEN
        RAISE EXCEPTION 'EMR 模板质量规则绑定数量不正确：expected=%, actual=%', seed_count, actual_count;
    END IF;

    FOR v_template_code, expected_count IN
        SELECT * FROM (VALUES
            ('internal_medicine', 107),
            ('tcm', 130),
            ('gynecology', 125),
            ('pediatrics', 127),
            ('dental', 120)
        ) AS expected_counts(code, count)
    LOOP
        SELECT COUNT(*)
        INTO actual_count
        FROM public.emr_template_quality_rules b
        JOIN public.emr_templates t ON t.id = b.template_id
        WHERE b.tenant_id = 0
          AND t.tenant_id = 0
          AND t.code = v_template_code;
        IF actual_count <> expected_count THEN
            RAISE EXCEPTION
                'EMR 模板质量规则绑定数量不正确：code=%, expected=%, actual=%',
                v_template_code, expected_count, actual_count;
        END IF;
    END LOOP;

    IF EXISTS (
        SELECT 1
        FROM public.emr_template_quality_rules b
        JOIN public.emr_templates t ON t.id = b.template_id
        WHERE b.tenant_id = 0
          AND t.tenant_id = 0
          AND t.code IN ('internal_medicine', 'tcm', 'gynecology', 'pediatrics', 'dental')
          AND NOT EXISTS (
              SELECT 1
              FROM _emr_seed_template_quality_rules s
              JOIN public.emr_templates st
                ON st.tenant_id = 0
               AND st.code = s.template_code
               AND st.status = 'published'
               AND st.is_current = TRUE
              JOIN public.emr_quality_rules sq
                ON sq.tenant_id = 0
               AND sq.code = s.quality_rule_code
               AND sq.status = 'published'
              WHERE st.id = b.template_id
                AND sq.id = b.quality_rule_id
          )
    ) THEN
        RAISE EXCEPTION 'EMR 模板质量规则绑定存在未被种子覆盖的记录';
    END IF;
END $$;
