-- 会员/身份折扣（按大类矩阵）：
-- 1) 患者价格身份：普通/会员/员工/家属/协议，默认普通；
-- 2) 租户级「身份 × 大类」折扣率矩阵（缺省补 1.0，药品锁定 1.0）；
-- 3) 收费单/收费行记录身份折扣与议价明细，便于逐行展示与退费按折后金额。
-- 全部为增量列/表，不改变 pending/paid/refunded 状态语义。

-- 1. 患者价格身份。
ALTER TABLE customers
    ADD COLUMN IF NOT EXISTS price_identity TEXT NOT NULL DEFAULT 'standard';
CREATE INDEX IF NOT EXISTS idx_customers_price_identity
    ON customers(tenant_id, price_identity);

-- 1.1 身份变更留痕（谁在何时把患者从哪个身份改成哪个身份）。
CREATE TABLE IF NOT EXISTS his_customer_identity_logs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL,
    old_identity TEXT NOT NULL DEFAULT '',
    new_identity TEXT NOT NULL DEFAULT '',
    operator_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_customer_identity_logs_customer
    ON his_customer_identity_logs(tenant_id, customer_id, id DESC);

-- 2. 身份 × 大类折扣率。rate 为折后比例（0.700 = 7 折）；药品由代码层锁定 1.0。
CREATE TABLE IF NOT EXISTS his_discount_rules (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    identity TEXT NOT NULL,
    category TEXT NOT NULL,
    rate NUMERIC(4,3) NOT NULL DEFAULT 1.000,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT his_discount_rules_rate_check CHECK (rate > 0 AND rate <= 1),
    CONSTRAINT his_discount_rules_uniq UNIQUE (tenant_id, identity, category)
);
CREATE INDEX IF NOT EXISTS idx_his_discount_rules_tenant
    ON his_discount_rules(tenant_id, identity);

-- 3. 议价上限（收费员可配；管理员/店长不受限）。rate 为最大议价比例（0.100 = 最多让 10%）。
CREATE TABLE IF NOT EXISTS his_discount_settings (
    tenant_id BIGINT PRIMARY KEY REFERENCES tenants(id),
    cashier_max_negotiation_rate NUMERIC(4,3) NOT NULL DEFAULT 0.100,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT his_discount_settings_rate_check
        CHECK (cashier_max_negotiation_rate >= 0 AND cashier_max_negotiation_rate <= 1)
);

-- 4. 收费单头：身份快照 + 议价快照 + 身份折扣合计。
ALTER TABLE his_orders
    ADD COLUMN IF NOT EXISTS price_identity TEXT NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS identity_discount_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS negotiation_mode TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS negotiation_value NUMERIC(12,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS negotiation_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS negotiation_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS negotiation_by BIGINT;

-- 5. 收费行：大类、身份折率、议价分摊额。payable = 原价 - 身份折扣 - 议价。
ALTER TABLE his_order_items
    ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS identity_rate NUMERIC(4,3) NOT NULL DEFAULT 1.000,
    ADD COLUMN IF NOT EXISTS negotiation_amount NUMERIC(12,2) NOT NULL DEFAULT 0;
