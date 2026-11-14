-- 018：临床科室只读字典（Phase 0 · 部门拍平的事实源）
--
-- 背景：科室 code 历史上有多套（EMR 长码 / HIS 短码 / emrquality 遗留码），
-- 本表是「只读对照表」，现有产品代码不改，仅供门户展示、迁移映射、跨产品报表读。
-- 统一 code 采用 EMR 长码（14 个科室全集）。

CREATE TABLE IF NOT EXISTS clinical_department_dict (
    id            bigserial PRIMARY KEY,
    code          text      NOT NULL,           -- 统一 code（EMR 长码）
    name          text      NOT NULL,           -- 中文名
    his_code      text,                         -- HIS 短码（无则 NULL）
    legacy_codes  text[]    NOT NULL DEFAULT '{}', -- 遗留 code（emrquality 等）
    template_code text,                         -- EMR 病历模板 code（无则 NULL）
    sort_order    int       NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_clinical_department_dict_code
    ON clinical_department_dict (code);

INSERT INTO clinical_department_dict (code, name, his_code, legacy_codes, template_code, sort_order) VALUES
    ('general_outpatient', '全科门诊',   NULL,           ARRAY['general'],      NULL,                  1),
    ('internal_medicine',  '内科',       'internal',     ARRAY[]::text[],       'internal_medicine',   2),
    ('surgery',            '外科',       'surgery',      ARRAY[]::text[],       NULL,                  3),
    ('orthopedics',        '骨科',       NULL,           ARRAY[]::text[],       NULL,                  4),
    ('gynecology',         '妇科',       'gyn',          ARRAY[]::text[],       'gynecology',          5),
    ('obstetrics',         '产科',       NULL,           ARRAY[]::text[],       NULL,                  6),
    ('pediatrics',         '儿科',       'pediatrics',   ARRAY[]::text[],       'pediatrics',          7),
    ('tcm',                '中医科',     'tcm',          ARRAY[]::text[],       'tcm',                 8),
    ('dental',             '口腔科',     'dental',       ARRAY['stomatology'],  'dental',              9),
    ('dermatology',        '皮肤科',     'dermatology',  ARRAY[]::text[],       NULL,                 10),
    ('ophthalmology',      '眼科',       NULL,           ARRAY[]::text[],       NULL,                 11),
    ('otolaryngology',     '耳鼻咽喉科', NULL,           ARRAY['ent'],          NULL,                 12),
    ('rehabilitation',     '康复科',     NULL,           ARRAY[]::text[],       NULL,                 13),
    ('emergency',          '急诊科',     NULL,           ARRAY[]::text[],       NULL,                 14)
ON CONFLICT (code) DO UPDATE
    SET name = EXCLUDED.name,
        his_code = EXCLUDED.his_code,
        legacy_codes = EXCLUDED.legacy_codes,
        template_code = EXCLUDED.template_code;
