-- 计费运营端第 3 批：开票记录 + 价目版本生效信息。
--
-- 开票口径（MVP）：
--   · 一张发票只归属一个机构；可选关联一张订单，关联订单时累计开票金额不能超过订单应收；
--   · 状态机 applied（待开票）→ issued（已开票）→ voided（已作废）；
--   · 发票号在「标记已开」时写入，允许为空（待开票）；非空发票号全局唯一；
--   · 已作废（voided）的发票不计入订单累计开票金额，作废后可重新开票；
--   · 已作废 / 已取消 / 已退款的订单不允许开票。
--
-- 价目版本新增 effective_from / approver / reason：
--   · effective_from 为空表示历史版本（立即生效），新版本按生效日参与价目选择；
--   · approver / reason 记录改价审批人与原因，改价同时写 system_action_logs。
--
-- 迁移可重复执行：表、索引、列都用 IF NOT EXISTS。

CREATE TABLE IF NOT EXISTS billing_invoices (
    id BIGSERIAL PRIMARY KEY,
    invoice_no VARCHAR(64) NOT NULL DEFAULT '',
    tenant_id BIGINT NOT NULL,
    order_id BIGINT REFERENCES billing_orders (id) ON DELETE SET NULL,
    amount NUMERIC(12,2) NOT NULL,
    invoice_type VARCHAR(16) NOT NULL DEFAULT '增值税普通',
    title TEXT NOT NULL DEFAULT '',
    tax_no VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'applied',
    issued_on DATE,
    note TEXT NOT NULL DEFAULT '',
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_invoices_tenant_check CHECK (tenant_id > 0),
    CONSTRAINT billing_invoices_amount_check CHECK (amount > 0),
    CONSTRAINT billing_invoices_type_check CHECK (invoice_type IN ('增值税普通', '增值税专用')),
    CONSTRAINT billing_invoices_status_check CHECK (status IN ('applied', 'issued', 'voided'))
);

-- 发票号允许在待开票阶段为空；一旦有号就必须全局唯一，空串不参与唯一约束。
CREATE UNIQUE INDEX IF NOT EXISTS billing_invoices_no_uq
    ON billing_invoices (invoice_no) WHERE invoice_no <> '';
CREATE INDEX IF NOT EXISTS billing_invoices_tenant_idx
    ON billing_invoices (tenant_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS billing_invoices_status_idx
    ON billing_invoices (status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS billing_invoices_order_idx
    ON billing_invoices (order_id) WHERE order_id IS NOT NULL;

ALTER TABLE IF EXISTS product_catalog
    ADD COLUMN IF NOT EXISTS effective_from DATE;
ALTER TABLE IF EXISTS product_catalog
    ADD COLUMN IF NOT EXISTS approver TEXT NOT NULL DEFAULT '';
ALTER TABLE IF EXISTS product_catalog
    ADD COLUMN IF NOT EXISTS reason TEXT NOT NULL DEFAULT '';

-- 价目按产品 + 档位 + 生效日选版本，生效日倒序能直接支撑「当前生效版本」查询。
CREATE INDEX IF NOT EXISTS product_catalog_effective_idx
    ON product_catalog (product_code, edition, effective_from DESC NULLS LAST);
