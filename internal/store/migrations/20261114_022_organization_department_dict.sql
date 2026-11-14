-- 022：平台组织字典（科室/职能部唯一真源）
--
-- 统一临床科室与职能部的标准定义，供运营台、门户、HIS、EMR 引用。
-- 本表是唯一真源；clinical_department_dict / billing_department_dict 迁移后归档。
-- type: clinical=临床科室, functional=职能部。

CREATE TABLE IF NOT EXISTS organization_department_dict (
    id            bigserial PRIMARY KEY,
    code          text        NOT NULL,
    name          text        NOT NULL,
    type          text        NOT NULL DEFAULT 'clinical',
    sort_order    int         NOT NULL DEFAULT 0,
    is_active     boolean     NOT NULL DEFAULT TRUE,
    template_code text,
    his_code      text,
    legacy_codes  text[]      NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_organization_department_dict_code
    ON organization_department_dict (code);
CREATE INDEX IF NOT EXISTS idx_organization_department_dict_type
    ON organization_department_dict (type, sort_order);

-- 1) 临床科室：从 clinical_department_dict 迁入（14 个，保留 code/模板/兼容映射）
INSERT INTO organization_department_dict (code, name, type, sort_order, template_code, his_code, legacy_codes)
SELECT code, name, 'clinical', sort_order, template_code, his_code, legacy_codes
FROM clinical_department_dict
ON CONFLICT (code) DO NOTHING;

-- 2) 补运营台字典里独有、但标准里缺的 2 个临床科室
INSERT INTO organization_department_dict (code, name, type, sort_order) VALUES
    ('medical_beauty', '医美科',  'clinical', 15),
    ('physical_exam',  '体检中心', 'clinical', 16)
ON CONFLICT (code) DO NOTHING;

-- 3) 把运营台历史名称收进 legacy_codes（用于名称匹配）
UPDATE organization_department_dict SET legacy_codes = legacy_codes || ARRAY['耳鼻喉科']::text[]
WHERE code = 'otolaryngology' AND NOT ('耳鼻喉科' = ANY(legacy_codes));
UPDATE organization_department_dict SET legacy_codes = legacy_codes || ARRAY['全科']::text[]
WHERE code = 'general_outpatient' AND NOT ('全科' = ANY(legacy_codes));
UPDATE organization_department_dict SET legacy_codes = legacy_codes || ARRAY['医疗美容科']::text[]
WHERE code = 'medical_beauty' AND NOT ('医疗美容科' = ANY(legacy_codes));

-- 4) 职能部（6 个）
INSERT INTO organization_department_dict (code, name, type, sort_order) VALUES
    ('consulting_dept',       '咨询部', 'functional', 1),
    ('customer_service_dept', '客服部', 'functional', 2),
    ('marketing_dept',        '市场部', 'functional', 3),
    ('medical_affairs_dept',  '医务科', 'functional', 4),
    ('operations_dept',       '运营部', 'functional', 5),
    ('it',                    '信息科', 'functional', 6)
ON CONFLICT (code) DO NOTHING;
