-- 017：患者（customers）拼音检索字段。
-- 患者列表 / 收费台队列支持中文姓名、全拼、首字母检索；存量数据由
-- cmd/his-pinyin-backfill -table patient 回填（Go 侧复用 his.PinyinOf）。
ALTER TABLE customers ADD COLUMN IF NOT EXISTS pinyin TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS initials TEXT NOT NULL DEFAULT '';

-- 与收费项目/药品拼音索引同口径：按租户 + 首字母/全拼前缀命中。
CREATE INDEX IF NOT EXISTS idx_customers_tenant_initials ON customers(tenant_id, initials);
CREATE INDEX IF NOT EXISTS idx_customers_tenant_pinyin ON customers(tenant_id, pinyin);
