-- HIS 采购单与采购明细。
-- 采购单先保存为草稿，收货时再按明细批量写入 his_stock / his_stock_movements，
-- 因此本迁移只新增表，不改共享表，也不触达 emr 域。

CREATE TABLE IF NOT EXISTS his_purchase_orders (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    order_no TEXT NOT NULL,
    supplier TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'ordered', 'received', 'cancelled')),
    total_amount NUMERIC(14,2) NOT NULL DEFAULT 0,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, order_no)
);

CREATE INDEX IF NOT EXISTS idx_his_purchase_orders_query
    ON his_purchase_orders(tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS his_purchase_order_items (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES his_purchase_orders(id) ON DELETE CASCADE,
    drug_id BIGINT NOT NULL REFERENCES his_drugs(id),
    drug_name TEXT NOT NULL DEFAULT '',
    quantity NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (quantity > 0),
    purchase_price NUMERIC(12,2) NOT NULL DEFAULT 0 CHECK (purchase_price >= 0),
    batch_no TEXT NOT NULL DEFAULT '',
    expiry_date DATE,
    amount NUMERIC(14,2) NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_his_purchase_order_items_order
    ON his_purchase_order_items(order_id);
CREATE INDEX IF NOT EXISTS idx_his_purchase_order_items_drug
    ON his_purchase_order_items(drug_id);
