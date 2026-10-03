-- 20260923_007_create_emr_icd10_catalog
-- Create ICD-10 diagnosis catalog table used by EMR submission checks.

CREATE TABLE IF NOT EXISTS public.emr_icd10_diagnosis_catalog (
    id BIGSERIAL PRIMARY KEY,
    catalog_code TEXT NOT NULL,
    catalog_name TEXT NOT NULL,
    catalog_version TEXT NOT NULL DEFAULT '',
    chapter_no TEXT NOT NULL DEFAULT '',
    chapter_code_range TEXT NOT NULL DEFAULT '',
    chapter_name TEXT NOT NULL DEFAULT '',
    section_code_range TEXT NOT NULL DEFAULT '',
    section_name TEXT NOT NULL DEFAULT '',
    category_code TEXT NOT NULL DEFAULT '',
    category_name TEXT NOT NULL DEFAULT '',
    subcategory_code TEXT NOT NULL DEFAULT '',
    subcategory_name TEXT NOT NULL DEFAULT '',
    diagnosis_code TEXT NOT NULL,
    diagnosis_name TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    source_file TEXT NOT NULL DEFAULT '',
    source_page INTEGER,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT emr_icd10_catalog_code_not_blank CHECK (BTRIM(catalog_code) <> ''),
    CONSTRAINT emr_icd10_diagnosis_code_not_blank CHECK (BTRIM(diagnosis_code) <> ''),
    CONSTRAINT emr_icd10_diagnosis_name_not_blank CHECK (BTRIM(diagnosis_name) <> ''),
    CONSTRAINT emr_icd10_normalized_name_not_blank CHECK (BTRIM(normalized_name) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS emr_icd10_catalog_code_diagnosis_code_uq
    ON public.emr_icd10_diagnosis_catalog (catalog_code, diagnosis_code);

CREATE INDEX IF NOT EXISTS emr_icd10_catalog_normalized_name_idx
    ON public.emr_icd10_diagnosis_catalog (catalog_code, normalized_name)
    WHERE is_active = TRUE;

CREATE INDEX IF NOT EXISTS emr_icd10_catalog_diagnosis_name_idx
    ON public.emr_icd10_diagnosis_catalog (catalog_code, diagnosis_name)
    WHERE is_active = TRUE;

COMMENT ON TABLE public.emr_icd10_diagnosis_catalog IS
    'ICD-10 医保诊断目录，用于电子病历诊断名称标准化检查。';

COMMENT ON COLUMN public.emr_icd10_diagnosis_catalog.normalized_name IS
    '用于严格匹配前的轻量归一化诊断名称，不用于模糊匹配或语义推断。';
