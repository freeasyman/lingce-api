-- 皮肤科 / 外科门诊病历模板种子（EMR 共享模板体系：HIS 工作站与听写病历共用）
--
-- 用法：
--   psql "$LINGCE_DEV_DB_URL" -v ON_ERROR_STOP=1 \
--     -f scripts/sql/20261113_seed_dermatology_surgery_templates.sql
--
-- 幂等：可重复执行。只新增/刷新 dermatology、surgery 两个模板及其 10 个专科字段，
-- 不修改任何现有模板。字段沿用平台既有做法放在 tenant_id=0 共享字段库
-- （与 gynecology_*、tcm_*、oral_* 等专科字段一致），模板按租户各建一份。
--
-- 目标租户：1（灵策医院）、5001（灵策综合医院）。

BEGIN;

-- ---------------------------------------------------------------------------
-- 1. 专科字段（tenant 0 共享字段库；code 全局唯一）
-- ---------------------------------------------------------------------------
INSERT INTO emr_fields
    (tenant_id, code, name, specialty, content_category, input_type,
     structure_json, options_json, description, status, created_by, updated_by)
VALUES
    (0, 'dermatology_lesion_site', '皮损部位', '皮肤科', '诊疗依据', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录皮损发生的解剖部位、分布范围、是否对称及累及面积。', 'enabled', 0, 0),
    (0, 'dermatology_lesion_morphology', '皮损形态', '皮肤科', '诊疗依据', '多选',
     '{}'::jsonb,
     '{"options":["红斑","丘疹","水疱","脓疱","斑块","结节","风团","脱屑","糜烂","溃疡","皲裂","色素沉着"]}'::jsonb,
     '记录皮损的原发和继发形态（红斑、丘疹、水疱、脱屑、糜烂等）、大小、颜色、边界、数目及排列方式。', 'enabled', 0, 0),
    (0, 'dermatology_itch_severity', '瘙痒程度', '皮肤科', '病史', '单选',
     '{}'::jsonb,
     '{"options":["无瘙痒","轻度","中度","重度"]}'::jsonb,
     '按无瘙痒、轻度、中度、重度记录瘙痒程度及对睡眠和日常生活的影响。', 'enabled', 0, 0),
    (0, 'dermatology_trigger', '诱因', '皮肤科', '病史', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录可疑诱因，包括接触物、食物、药物、环境、季节、精神因素及既往类似发作情况。', 'enabled', 0, 0),
    (0, 'dermatology_specialty_exam', '皮肤科专科检查', '皮肤科', '诊疗依据', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录皮肤科专科查体：皮损部位、形态、大小、颜色、边界、数目、分布，以及黏膜、毛发、甲受累情况。', 'enabled', 0, 0),

    (0, 'surgery_injury_site', '受伤/发病部位', '外科', '诊疗依据', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录受伤或发病的解剖部位、侧别及范围。', 'enabled', 0, 0),
    (0, 'surgery_wound', '伤口情况', '外科', '诊疗依据', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录伤口大小、深度、边缘、污染程度、渗出、出血、异物及周围组织情况。', 'enabled', 0, 0),
    (0, 'surgery_tenderness_mobility', '压痛/活动度', '外科', '诊疗依据', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录局部压痛部位与程度、关节或肢体活动度、感觉及血运情况。', 'enabled', 0, 0),
    (0, 'surgery_treatment_method', '处理方式', '外科', '医疗措施', '多选',
     '{}'::jsonb,
     '{"options":["清创","包扎","换药","引流","缝合","固定","其他"]}'::jsonb,
     '记录本次处理方式（清创、包扎、换药、引流、缝合、固定等）及操作要点。', 'enabled', 0, 0),
    (0, 'surgery_specialty_exam', '外科专科检查', '外科', '诊疗依据', '长文本',
     '{}'::jsonb, '{}'::jsonb,
     '记录外科专科查体：视诊、触诊、压痛、包块、活动度、伤口及周围组织情况。', 'enabled', 0, 0)
ON CONFLICT (tenant_id, code) DO UPDATE
SET name             = EXCLUDED.name,
    specialty        = EXCLUDED.specialty,
    content_category = EXCLUDED.content_category,
    input_type       = EXCLUDED.input_type,
    structure_json   = EXCLUDED.structure_json,
    options_json     = EXCLUDED.options_json,
    description      = EXCLUDED.description,
    status           = EXCLUDED.status,
    updated_by       = 0,
    updated_at       = NOW();

-- ---------------------------------------------------------------------------
-- 2. 模板主记录（tenant 1 / 5001 各一份，published + is_current，version_no=v1）
-- ---------------------------------------------------------------------------
-- 防御：若同一 code 下存在历史 current 版本，先取消，避免触发 current 唯一索引。
UPDATE emr_templates
SET is_current = FALSE, updated_at = NOW()
WHERE tenant_id IN (1, 5001)
  AND code IN ('dermatology', 'surgery')
  AND version_no <> 'v1'
  AND is_current = TRUE;

INSERT INTO emr_templates
    (tenant_id, code, name, version_no, document_type, visit_type, department_id,
     specialty, status, print_title, description, created_by, updated_by,
     published_at, published_by, is_current)
SELECT v.tenant_id, v.code, v.name, 'v1', '门诊病历', '初诊', NULL,
       v.specialty, 'published', v.print_title, v.description, 0, 0,
       NOW(), 0, TRUE
FROM (VALUES
    (1::bigint,    'dermatology', '皮肤科门诊病历', '皮肤科', '皮肤科门诊病历',
     '皮肤科门诊初诊病历模板，包含皮损部位、皮损形态、瘙痒程度、诱因等专科栏位。'),
    (5001::bigint, 'dermatology', '皮肤科门诊病历', '皮肤科', '皮肤科门诊病历',
     '皮肤科门诊初诊病历模板，包含皮损部位、皮损形态、瘙痒程度、诱因等专科栏位。'),
    (1::bigint,    'surgery',     '外科门诊病历',   '外科',   '外科门诊病历',
     '外科门诊初诊病历模板，包含受伤/发病部位、伤口情况、压痛/活动度、处理方式等专科栏位。'),
    (5001::bigint, 'surgery',     '外科门诊病历',   '外科',   '外科门诊病历',
     '外科门诊初诊病历模板，包含受伤/发病部位、伤口情况、压痛/活动度、处理方式等专科栏位。')
) AS v(tenant_id, code, name, specialty, print_title, description)
ON CONFLICT (tenant_id, code, version_no) DO UPDATE
SET name          = EXCLUDED.name,
    document_type = EXCLUDED.document_type,
    visit_type    = EXCLUDED.visit_type,
    specialty     = EXCLUDED.specialty,
    status        = 'published',
    print_title   = EXCLUDED.print_title,
    description   = EXCLUDED.description,
    is_current    = TRUE,
    updated_by    = 0,
    published_at  = COALESCE(emr_templates.published_at, NOW()),
    published_by  = COALESCE(emr_templates.published_by, 0),
    updated_at    = NOW();

-- ---------------------------------------------------------------------------
-- 3. 模板字段绑定（先清空本模板绑定，再按顺序重建；只动 dermatology/surgery）
-- ---------------------------------------------------------------------------
DELETE FROM emr_template_fields tf
USING emr_templates t
WHERE tf.template_id = t.id
  AND t.tenant_id IN (1, 5001)
  AND t.code IN ('dermatology', 'surgery');

INSERT INTO emr_template_fields
    (tenant_id, template_id, field_id, display_order, is_visible, is_required, description, created_by)
SELECT 0, t.id, f.id, b.display_order, TRUE, b.is_required, f.name, 0
FROM (VALUES
    -- 皮肤科：主诉 → 现病史 → 既往史 → 过敏史 → 体格检查 → 专科 → 诊断 → 处理 → 医嘱
    ('dermatology', 'chief_complaint',             10, FALSE),
    ('dermatology', 'present_illness',             20, FALSE),
    ('dermatology', 'past_history',                30, FALSE),
    ('dermatology', 'allergy_history',             40, FALSE),
    ('dermatology', 'physical_exam',               50, FALSE),
    ('dermatology', 'dermatology_specialty_exam',  60, FALSE),
    ('dermatology', 'dermatology_lesion_site',     70, FALSE),
    ('dermatology', 'dermatology_lesion_morphology', 80, FALSE),
    ('dermatology', 'dermatology_itch_severity',   90, FALSE),
    ('dermatology', 'dermatology_trigger',        100, FALSE),
    ('dermatology', 'diagnosis',                  110, TRUE),
    ('dermatology', 'disposition',                120, FALSE),
    ('dermatology', 'medical_advice',             130, FALSE),

    -- 外科：主诉 → 现病史 → 既往史 → 过敏史 → 体格检查 → 专科 → 诊断 → 处理 → 医嘱
    ('surgery', 'chief_complaint',          10, FALSE),
    ('surgery', 'present_illness',          20, FALSE),
    ('surgery', 'past_history',             30, FALSE),
    ('surgery', 'allergy_history',          40, FALSE),
    ('surgery', 'physical_exam',            50, FALSE),
    ('surgery', 'surgery_specialty_exam',   60, FALSE),
    ('surgery', 'surgery_injury_site',      70, FALSE),
    ('surgery', 'surgery_wound',            80, FALSE),
    ('surgery', 'surgery_tenderness_mobility', 90, FALSE),
    ('surgery', 'diagnosis',               100, TRUE),
    ('surgery', 'surgery_treatment_method', 110, FALSE),
    ('surgery', 'disposition',             120, FALSE),
    ('surgery', 'medical_advice',          130, FALSE)
) AS b(template_code, field_code, display_order, is_required)
JOIN emr_templates t
  ON t.tenant_id IN (1, 5001)
 AND t.code = b.template_code
 AND t.version_no = 'v1'
JOIN emr_fields f
  ON f.tenant_id = 0
 AND f.code = b.field_code
 AND f.status = 'enabled';

-- ---------------------------------------------------------------------------
-- 4. 结果校验输出
-- ---------------------------------------------------------------------------
SELECT t.tenant_id,
       t.code,
       t.name,
       t.version_no,
       t.status,
       t.is_current,
       COUNT(tf.id) AS field_count,
       string_agg(f.code, ' → ' ORDER BY tf.display_order) AS field_order
FROM emr_templates t
LEFT JOIN emr_template_fields tf ON tf.template_id = t.id
LEFT JOIN emr_fields f ON f.id = tf.field_id
WHERE t.tenant_id IN (1, 5001)
  AND t.code IN ('dermatology', 'surgery')
GROUP BY t.tenant_id, t.code, t.name, t.version_no, t.status, t.is_current
ORDER BY t.tenant_id, t.code;

COMMIT;

-- ---------------------------------------------------------------------------
-- 回滚（按需取消注释执行；只删除本脚本新增的模板与字段绑定，
-- 字段仅在未被任何其他模板引用时删除）
-- ---------------------------------------------------------------------------
-- BEGIN;
-- DELETE FROM emr_template_fields tf
-- USING emr_templates t
-- WHERE tf.template_id = t.id
--   AND t.tenant_id IN (1, 5001)
--   AND t.code IN ('dermatology', 'surgery');
-- DELETE FROM emr_templates
-- WHERE tenant_id IN (1, 5001)
--   AND code IN ('dermatology', 'surgery');
-- DELETE FROM emr_fields f
-- WHERE f.tenant_id = 0
--   AND f.code IN (
--     'dermatology_lesion_site', 'dermatology_lesion_morphology',
--     'dermatology_itch_severity', 'dermatology_trigger', 'dermatology_specialty_exam',
--     'surgery_injury_site', 'surgery_wound', 'surgery_tenderness_mobility',
--     'surgery_treatment_method', 'surgery_specialty_exam')
--   AND NOT EXISTS (SELECT 1 FROM emr_template_fields tf WHERE tf.field_id = f.id);
-- COMMIT;
