-- 字典拼音检索：收费项目/药品加全拼与首字母；ICD 目录用独立拼音索引表（不动 EMR 表结构）。
ALTER TABLE his_charge_items ADD COLUMN IF NOT EXISTS pinyin TEXT NOT NULL DEFAULT '';
ALTER TABLE his_charge_items ADD COLUMN IF NOT EXISTS initials TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_his_charge_items_initials ON his_charge_items(tenant_id, initials);

ALTER TABLE his_drugs ADD COLUMN IF NOT EXISTS pinyin TEXT NOT NULL DEFAULT '';
ALTER TABLE his_drugs ADD COLUMN IF NOT EXISTS initials TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_his_drugs_initials ON his_drugs(tenant_id, initials);

CREATE TABLE IF NOT EXISTS his_icd10_pinyin (
    catalog_id BIGINT PRIMARY KEY,
    pinyin TEXT NOT NULL DEFAULT '',
    initials TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_his_icd10_pinyin_initials ON his_icd10_pinyin(initials);
