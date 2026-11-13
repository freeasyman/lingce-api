-- 收费链路 P0/P1：
-- 1) 收费项目「随项目带出」绑定（父项目 → 子耗材/材料）；
-- 2) 按项退费明细（his_refunds 保留为一次退费单头，his_refund_items 记录退到哪些行）；
-- 3) 治疗医嘱回写生成的收费单；
-- 4) 单据/行级已退金额与数量，供收费台展示与重收判断。
-- 全部为增量列/表，不改变既有 pending/paid/refunded 状态语义。

-- 1. 收费项目绑定：父子均限本租户，禁止自绑定，数量必须为正。
CREATE TABLE IF NOT EXISTS his_charge_item_links (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    parent_charge_item_id BIGINT NOT NULL REFERENCES his_charge_items(id) ON DELETE CASCADE,
    child_charge_item_id BIGINT NOT NULL REFERENCES his_charge_items(id) ON DELETE CASCADE,
    quantity NUMERIC(12,2) NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT his_charge_item_links_qty_check CHECK (quantity > 0),
    CONSTRAINT his_charge_item_links_no_self_check CHECK (parent_charge_item_id <> child_charge_item_id),
    CONSTRAINT his_charge_item_links_uniq UNIQUE (tenant_id, parent_charge_item_id, child_charge_item_id)
);
CREATE INDEX IF NOT EXISTS idx_his_charge_item_links_parent
    ON his_charge_item_links(tenant_id, parent_charge_item_id);

-- 2. 按项退费明细：一次退费（his_refunds）可覆盖多行。
CREATE TABLE IF NOT EXISTS his_refund_items (
    id BIGSERIAL PRIMARY KEY,
    refund_id BIGINT NOT NULL REFERENCES his_refunds(id) ON DELETE CASCADE,
    order_item_id BIGINT NOT NULL REFERENCES his_order_items(id),
    quantity NUMERIC(12,2) NOT NULL DEFAULT 0,
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT his_refund_items_qty_check CHECK (quantity > 0),
    CONSTRAINT his_refund_items_amount_check CHECK (amount >= 0)
);
CREATE INDEX IF NOT EXISTS idx_his_refund_items_refund ON his_refund_items(refund_id);
CREATE INDEX IF NOT EXISTS idx_his_refund_items_order_item ON his_refund_items(order_item_id);

-- 3. 单据/行级退费累计：paid_total 保持「历史实收」口径，refunded_total 记录累计退款；
--    净实收 = paid_total - refunded_total。refunded_quantity 用于按项可退数量计算。
ALTER TABLE his_orders
    ADD COLUMN IF NOT EXISTS refunded_total NUMERIC(12,2) NOT NULL DEFAULT 0;
ALTER TABLE his_order_items
    ADD COLUMN IF NOT EXISTS refunded_quantity NUMERIC(12,2) NOT NULL DEFAULT 0;

-- 4. 治疗医嘱回写自动生成的收费单（取消/退费时按 order_id + charge_item_id 反查）。
ALTER TABLE his_treatment_orders
    ADD COLUMN IF NOT EXISTS order_id BIGINT NULL;
CREATE INDEX IF NOT EXISTS idx_his_treatment_orders_order
    ON his_treatment_orders(order_id);
