-- 013：拼音字段兜底 + 未付单结账按当前身份重算留痕。
-- 拼音字段与索引在 009 已存在，这里仅做幂等兜底（兼容未执行 009 的环境）。
ALTER TABLE his_charge_items ADD COLUMN IF NOT EXISTS pinyin TEXT NOT NULL DEFAULT '';
ALTER TABLE his_charge_items ADD COLUMN IF NOT EXISTS initials TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_his_charge_items_initials ON his_charge_items(tenant_id, initials);

ALTER TABLE his_drugs ADD COLUMN IF NOT EXISTS pinyin TEXT NOT NULL DEFAULT '';
ALTER TABLE his_drugs ADD COLUMN IF NOT EXISTS initials TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_his_drugs_initials ON his_drugs(tenant_id, initials);

-- 结账重算留痕：记录被重算订单的原/新身份与原/新应收，便于对账与追溯。
CREATE TABLE IF NOT EXISTS his_order_repricing_logs (
    id               BIGSERIAL PRIMARY KEY,
    tenant_id        BIGINT NOT NULL,
    order_id         BIGINT NOT NULL,
    customer_id      BIGINT NOT NULL,
    previous_identity TEXT NOT NULL DEFAULT '',
    new_identity     TEXT NOT NULL DEFAULT '',
    previous_payable NUMERIC(12,2) NOT NULL DEFAULT 0,
    payable_total    NUMERIC(12,2) NOT NULL DEFAULT 0,
    operator_id      BIGINT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_order_repricing_logs_order
    ON his_order_repricing_logs(tenant_id, order_id);
