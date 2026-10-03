-- 统一门诊病历模板编码，并初始化平台默认的五套医疗科室模板。
--
-- 医生医疗科室编码直接作为 emr_templates.code：
-- internal_medicine、tcm、gynecology、pediatrics、dental。
--
-- 版本模型：emr_templates 一行代表一个模板版本，is_current=true
-- 且 status='published' 的行才是当前可使用版本。
--
-- 本迁移不读取前端仓库文件，避免生产环境依赖 lingce-web。
-- 缺失的平台默认模板、栏位和模板栏位关系由本迁移原子创建；
-- 已存在的租户配置不被覆盖。
--
-- 如果同一租户同时存在新旧编码，或存在多个无法明确选择的已发布版本，
-- 迁移直接失败，不按创建时间猜测。
--
-- 署名：Codex
-- 时间：2026-09-13

-- 开发库曾使用过不同版本的输入类型枚举。先删除所有实际绑定
-- emr_fields.input_type 的检查约束，避免后续转换旧值时被旧约束拦截。
DO $$
DECLARE
    constraint_name TEXT;
BEGIN
    FOR constraint_name IN
        SELECT c.conname
        FROM pg_constraint c
        JOIN pg_class r ON r.oid = c.conrelid
        JOIN pg_namespace n ON n.oid = r.relnamespace
        WHERE n.nspname = 'public'
          AND r.relname = 'emr_fields'
          AND c.contype = 'c'
          AND pg_get_constraintdef(c.oid) ILIKE '%input_type%'
    LOOP
        EXECUTE format(
            'ALTER TABLE public.emr_fields DROP CONSTRAINT %I',
            constraint_name
        );
    END LOOP;
END $$;

CREATE TEMP TABLE _emr_seed_templates (
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    specialty TEXT NOT NULL,
    print_title TEXT NOT NULL,
    description TEXT NOT NULL
) ON COMMIT DROP;

INSERT INTO _emr_seed_templates (code, name, specialty, print_title, description)
VALUES
    ('internal_medicine', '内科门诊病历', '内科', '内科门诊病历', '平台默认内科门诊病历模板。'),
    ('tcm', '中医门诊病历', '中医', '中医门诊病历', '平台默认中医门诊病历模板，包含中医四诊、辨证和治法栏位。'),
    ('gynecology', '妇科门诊病历', '妇科', '妇科门诊病历', '平台默认妇科门诊病历模板，包含月经史、婚育史和孕产史栏位。'),
    ('pediatrics', '儿科门诊病历', '儿科', '儿科门诊病历', '平台默认儿科门诊病历模板，包含出生史、喂养史和儿童用药依据栏位。'),
    ('dental', '口腔门诊病历', '口腔', '口腔门诊病历', '平台默认口腔门诊病历模板，包含牙位、口腔专科检查和治疗计划栏位.');

CREATE TEMP TABLE _emr_seed_fields (
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    specialty TEXT,
    content_category TEXT NOT NULL,
    input_type TEXT NOT NULL,
    description TEXT NOT NULL,
    display_order INTEGER NOT NULL
) ON COMMIT DROP;

INSERT INTO _emr_seed_fields
    (code, name, specialty, content_category, input_type, description, display_order)
VALUES
    ('chief_complaint', '主诉', NULL, '病史', '长文本', '记录患者就诊的主要症状、体征、部位和持续时间。', 10),
    ('present_illness', '现病史', NULL, '病史', '长文本', '记录本次疾病的起病、发展、伴随症状、既往处理及效果。', 20),
    ('past_history', '既往史', NULL, '病史', '长文本', '记录既往疾病、慢性病、手术、住院和重要治疗史。', 30),
    ('personal_history', '个人史', NULL, '病史', '长文本', '记录与本次就诊相关的个人生活、职业和暴露信息。', 40),
    ('family_history', '家族史', NULL, '病史', '长文本', '记录与当前疾病相关的家族疾病和遗传风险信息。', 50),
    ('allergy_history', '过敏史', NULL, '病史', '长文本', '记录药物、食物及其他过敏原，以及明确无过敏或尚未询问的状态。', 60),
    ('physical_exam', '体格检查', NULL, '诊疗依据', '结构化内容', '记录生命体征、一般状态、阳性体征、必要阴性体征和专科查体。', 70),
    ('auxiliary_exam', '辅助检查', NULL, '诊疗依据', '结构化内容', '记录本次就诊已有或新安排的化验、影像及其他辅助检查。', 80),
    ('diagnosis', '诊断', NULL, '医疗判断', '长文本', '记录本次就诊形成的西医诊断或其他适用诊断。', 90),
    ('prescription', '处方', NULL, '医疗措施', '结构化内容', '记录药品名称、规格、用法、用量、频次、疗程和数量。', 100),
    ('disposition', '处置', NULL, '医疗措施', '结构化内容', '记录检查申请、治疗操作、输液、注射和其他医疗处置。', 110),
    ('medical_advice', '医嘱', NULL, '医疗措施', '长文本', '记录用药、饮食、生活、观察和异常情况处理指导。', 120),
    ('followup', '复诊安排', NULL, '医疗措施', '长文本', '记录复诊时间、复诊所需资料和需要及时复诊的情况。', 130),
    ('supplement', '补充说明', NULL, '医疗措施', '长文本', '记录无法归入其他正式栏位、但确有保留必要的补充内容。', 140),
    ('menstrual_history', '月经史', '妇科', '病史', '结构化内容', '记录月经周期、经期、经量、末次月经及痛经等信息。', 210),
    ('last_menstrual_period', '末次月经', '妇科', '病史', '日期', '记录患者末次月经的日期。', 220),
    ('marital_reproductive_history', '婚育史', '妇科', '病史', '结构化内容', '记录婚姻、生育、妊娠和分娩相关历史。', 230),
    ('obstetric_history', '孕产史', '妇科', '病史', '结构化内容', '记录历次妊娠、分娩、流产及围产期情况。', 240),
    ('gynecology_specialty_exam', '妇科专科查体', '妇科', '诊疗依据', '长文本', '记录妇科相关专科体格检查结果。', 250),
    ('gynecology_auxiliary_exam', '妇科相关辅助检查', '妇科', '诊疗依据', '结构化内容', '记录妇科相关化验、超声、影像和其他检查结果。', 260),
    ('tcm_diagnosis', '中医诊断', '中医', '医疗判断', '长文本', '记录中医病名、证候及相关诊断。', 310),
    ('tcm_four_examinations', '中医四诊', '中医', '诊疗依据', '结构化内容', '记录望、闻、问、切四诊信息。', 320),
    ('tcm_inspection', '望诊', '中医', '诊疗依据', '长文本', '记录面色、形态、舌象等望诊所见。', 330),
    ('tcm_auscultation_olfaction', '闻诊', '中医', '诊疗依据', '长文本', '记录声音、气味等闻诊所见。', 340),
    ('tcm_inquiry', '问诊', '中医', '病史', '长文本', '记录围绕寒热、汗、饮食、睡眠、二便及相关症状的问诊内容。', 350),
    ('tcm_palpation', '切诊', '中医', '诊疗依据', '长文本', '记录脉诊及其他切诊所见。', 360),
    ('tcm_tongue', '舌象', '中医', '诊疗依据', '结构化内容', '记录舌质、舌苔及相关舌象描述。', 370),
    ('tcm_pulse', '脉象', '中医', '诊疗依据', '长文本', '记录脉象及脉诊相关描述。', 380),
    ('tcm_pattern_differentiation', '中医辨证', '中医', '医疗判断', '长文本', '记录基于四诊资料形成的证候判断。', 390),
    ('tcm_treatment_principle', '治法', '中医', '医疗判断', '长文本', '记录中医治疗原则和治法。', 400),
    ('tcm_herbal_prescription', '中药处方', '中医', '医疗措施', '结构化内容', '记录中药饮片、剂量、用法、频次和疗程。', 410),
    ('oral_tooth_position', '牙位', '口腔', '就诊归属', '结构化内容', '记录涉及的牙位、牙面或区域。', 510),
    ('oral_specialty_exam', '口腔专科检查', '口腔', '诊疗依据', '长文本', '记录口腔专科检查所见。', 520),
    ('oral_periodontal_or_dental_condition', '牙周或牙体情况', '口腔', '诊疗依据', '长文本', '记录牙周组织或牙体组织的主要病变情况。', 530),
    ('oral_imaging_findings', '影像所见', '口腔', '诊疗依据', '长文本', '记录口腔影像检查所见及其来源。', 540),
    ('oral_treatment_plan', '口腔治疗计划', '口腔', '医疗措施', '长文本', '记录口腔治疗方案、分期安排和患者沟通结果。', 550),
    ('pediatric_birth_history', '出生史', '儿科', '病史', '结构化内容', '记录出生孕周、分娩方式、出生体重及围产期情况。', 610),
    ('pediatric_feeding_history', '喂养史', '儿科', '病史', '结构化内容', '记录母乳、配方、辅食、饮食和喂养困难等情况。', 620),
    ('pediatric_guardian_statement', '监护人陈述', '儿科', '病史', '长文本', '记录监护人对患儿病情、用药和既往情况的陈述。', 630),
    ('pediatric_weight', '儿童体重', '儿科', '诊疗依据', '数值', '记录儿童当前体重及测量单位，供病情判断和用药依据使用。', 640),
    ('pediatric_medication_basis', '儿童特殊用药依据', '儿科', '医疗措施', '结构化内容', '记录儿童用药与年龄、体重、剂量计算及调整依据。', 650);

CREATE TEMP TABLE _emr_seed_template_fields (
    template_code TEXT NOT NULL,
    field_code TEXT NOT NULL,
    display_order INTEGER NOT NULL
) ON COMMIT DROP;

INSERT INTO _emr_seed_template_fields (template_code, field_code, display_order)
SELECT template_code, field_code, display_order
FROM (
    SELECT
        templates.template_code,
        fields.code AS field_code,
        fields.display_order
    FROM
        (VALUES
            ('internal_medicine'), ('tcm'), ('gynecology'), ('pediatrics'), ('dental')
        ) AS templates(template_code)
    JOIN _emr_seed_fields fields
      ON fields.specialty IS NULL
      OR fields.specialty = CASE templates.template_code
          WHEN 'tcm' THEN '中医'
          WHEN 'gynecology' THEN '妇科'
          WHEN 'pediatrics' THEN '儿科'
          WHEN 'dental' THEN '口腔'
          ELSE '内科'
      END
) seeded;

UPDATE public.emr_templates existing
SET code = seeded.code,
    updated_at = NOW()
FROM _emr_seed_templates seeded
WHERE existing.code = seeded.code || '_outpatient'
  AND NOT EXISTS (
      SELECT 1
      FROM public.emr_templates newer
      WHERE COALESCE(newer.tenant_id, 0) = COALESCE(existing.tenant_id, 0)
        AND newer.code = seeded.code
  );

DO $$
DECLARE
    seed RECORD;
    old_code TEXT;
    published_count INTEGER;
    current_count INTEGER;
    current_published_count INTEGER;
BEGIN
    FOR seed IN SELECT * FROM _emr_seed_templates LOOP
        old_code := seed.code || '_outpatient';

        IF EXISTS (
            SELECT 1
            FROM public.emr_templates newer
            JOIN public.emr_templates older
              ON COALESCE(newer.tenant_id, 0) = COALESCE(older.tenant_id, 0)
             AND newer.code = seed.code
             AND older.code = old_code
        ) THEN
            RAISE EXCEPTION
                'EMR 模板新旧编码同时存在，无法自动迁移：%, %',
                seed.code, old_code;
        END IF;

        INSERT INTO public.emr_templates
            (tenant_id, code, name, version_no, document_type, visit_type,
             specialty, status, is_current, print_title, description,
             created_by, updated_by, published_at, published_by)
        SELECT
            0, seed.code, seed.name, '1.0', '门诊病历', '通用',
            seed.specialty, 'published', TRUE, seed.print_title, seed.description,
            0, 0, NOW(), 0
        WHERE NOT EXISTS (
            SELECT 1
            FROM public.emr_templates existing
            WHERE COALESCE(existing.tenant_id, 0) = 0
              AND existing.code = seed.code
              AND existing.version_no = '1.0'
        );

        SELECT COUNT(*)
        INTO published_count
        FROM public.emr_templates
        WHERE COALESCE(tenant_id, 0) = 0
          AND code = seed.code
          AND status = 'published';

        SELECT COUNT(*)
        INTO current_count
        FROM public.emr_templates
        WHERE COALESCE(tenant_id, 0) = 0
          AND code = seed.code
          AND is_current = TRUE;

        SELECT COUNT(*)
        INTO current_published_count
        FROM public.emr_templates
        WHERE COALESCE(tenant_id, 0) = 0
          AND code = seed.code
          AND status = 'published'
          AND is_current = TRUE;

        IF published_count = 0 THEN
            RAISE EXCEPTION
                'EMR 平台模板没有已发布版本：code=%', seed.code;
        END IF;
        IF current_count > 1 OR current_published_count > 1 THEN
            RAISE EXCEPTION
                'EMR 平台模板存在多个当前版本：code=%', seed.code;
        END IF;
        IF current_count = 0 THEN
            IF published_count <> 1 THEN
                RAISE EXCEPTION
                    'EMR 平台模板没有明确当前版本，且存在多个已发布版本：code=%',
                    seed.code;
            END IF;
            UPDATE public.emr_templates
            SET is_current = TRUE, updated_at = NOW()
            WHERE tenant_id = 0
              AND code = seed.code
              AND status = 'published';
        ELSIF current_published_count = 0 THEN
            RAISE EXCEPTION
                'EMR 平台当前版本不是已发布版本：code=%', seed.code;
        END IF;
    END LOOP;
END $$;

DO $$
DECLARE
    seed RECORD;
BEGIN
    -- 开发库中 emr_fields 可能保留过早期的英文输入类型值。
    -- 先统一已有值，再把栏位表约束收敛到当前确认的数据模型枚举。
    UPDATE public.emr_fields
    SET input_type = CASE BTRIM(input_type)
        WHEN 'text' THEN '文本'
        WHEN 'long_text' THEN '长文本'
        WHEN 'textarea' THEN '长文本'
        WHEN 'single_select' THEN '单选'
        WHEN 'select' THEN '单选'
        WHEN 'multi_select' THEN '多选'
        WHEN 'multiselect' THEN '多选'
        WHEN 'date' THEN '日期'
        WHEN 'number' THEN '数值'
        WHEN 'structured' THEN '结构化内容'
        ELSE BTRIM(input_type)
    END
    WHERE input_type IS NOT NULL;

    IF EXISTS (
        SELECT 1
        FROM public.emr_fields
        WHERE input_type NOT IN ('文本', '长文本', '单选', '多选', '日期', '数值', '结构化内容')
    ) THEN
        RAISE EXCEPTION
            'emr_fields 存在无法识别的 input_type，不能安全迁移';
    END IF;

    FOR seed IN SELECT * FROM _emr_seed_fields LOOP
        UPDATE public.emr_fields
        SET name = seed.name,
            specialty = seed.specialty,
            content_category = seed.content_category,
            input_type = seed.input_type,
            description = seed.description,
            status = 'enabled',
            updated_at = NOW(),
            updated_by = 0
        WHERE tenant_id = 0
          AND code = seed.code;

        IF NOT FOUND THEN
            INSERT INTO public.emr_fields
                (tenant_id, code, name, specialty, content_category, input_type,
                 description, options_json, structure_json, status, created_by, updated_by)
            VALUES
                (0, seed.code, seed.name, seed.specialty, seed.content_category,
                 seed.input_type, seed.description, '{}'::jsonb, '{}'::jsonb,
                 'enabled', 0, 0);
        END IF;
    END LOOP;
END $$;

ALTER TABLE public.emr_fields
    ADD CONSTRAINT emr_fields_input_type_check
    CHECK (input_type IN ('文本', '长文本', '单选', '多选', '日期', '数值', '结构化内容'));

DO $$
DECLARE
    seed RECORD;
    v_template_id UUID;
    v_field_id UUID;
BEGIN
    FOR seed IN SELECT * FROM _emr_seed_template_fields LOOP
        SELECT t.id
        INTO v_template_id
        FROM public.emr_templates t
        WHERE t.tenant_id = 0
          AND t.code = seed.template_code
          AND t.status = 'published'
          AND t.is_current = TRUE;

        SELECT f.id
        INTO v_field_id
        FROM public.emr_fields f
        WHERE f.tenant_id = 0
          AND f.code = seed.field_code
          AND f.status = 'enabled';

        IF v_template_id IS NULL OR v_field_id IS NULL THEN
            RAISE EXCEPTION
                'EMR 模板栏位初始化失败：template=%, field=%',
                seed.template_code, seed.field_code;
        END IF;

        INSERT INTO public.emr_template_fields
            (tenant_id, template_id, field_id, display_order, is_visible,
             is_required, description, created_by)
        SELECT
            0, v_template_id, v_field_id, seed.display_order, TRUE,
            FALSE, '', 0
        WHERE NOT EXISTS (
            SELECT 1
            FROM public.emr_template_fields existing
            WHERE existing.tenant_id = 0
              AND existing.template_id = v_template_id
              AND existing.field_id = v_field_id
        );
    END LOOP;
END $$;
