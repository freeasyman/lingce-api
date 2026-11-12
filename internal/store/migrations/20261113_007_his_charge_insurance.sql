-- 收费项目补医保字段：医保类别（甲/乙/丙/自费）与医保编码。
-- 字典来源：灵策门诊产品数据（~/Documents/lingce-data/医保收费字典），客户数据仅作来源之一。
ALTER TABLE his_charge_items ADD COLUMN IF NOT EXISTS insurance_category TEXT NOT NULL DEFAULT '';
ALTER TABLE his_charge_items ADD COLUMN IF NOT EXISTS insurance_code TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_his_charge_items_insurance ON his_charge_items(tenant_id, insurance_code);
