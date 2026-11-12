-- 运营端计费：收款单 + 核销明细（幂等）。
--
-- 产品文档 4.4：收款单支持「一笔对多单」——付款方 / 金额 / 到账日 / 方式 / 凭证号 / 备注，
-- 核销可跨租户、可部分，未核销余额要显著提示。
--
-- 设计要点：
--   · billing_payments 记钱本身；billing_payment_allocations 记「这笔钱核销到哪张订单多少」；
--   · 核销金额必须在 (0, 订单未收金额] 之间（CHECK amount > 0 兜底正数，上限由服务层校验）；
--   · 核销后同事务回写 billing_orders.paid_amount 与状态（created/paid/settled），
--     订单侧应收与收款保持自洽；
--   · (payment_id, order_id) 唯一：同一笔收款对同一订单只核销一次，
--     重复提交按幂等处理（金额相同直接返回，不同则 409），不会把 paid_amount 加两次；
--   · 收款单状态：unallocated 未核销 / partial 部分核销 / allocated 已核销 / void 已作废。
--     status 由核销合计推导，服务层在每次核销后回写。
--
-- order_id 引用 billing_orders(id)：两张表都是本模块新建/既有的计费表，
-- 不存在历史脏数据，外键可以保证核销不会指向不存在的订单。

CREATE TABLE IF NOT EXISTS billing_payments (
    id BIGSERIAL PRIMARY KEY,
    payment_no VARCHAR(32) NOT NULL,
    payer_name TEXT NOT NULL DEFAULT '',
    amount NUMERIC(12,2) NOT NULL,
    received_on DATE NOT NULL,
    method TEXT NOT NULL DEFAULT '',
    voucher_no TEXT NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'unallocated',
    note TEXT NOT NULL DEFAULT '',
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_payments_amount_check CHECK (amount > 0),
    CONSTRAINT billing_payments_status_check CHECK (status IN ('unallocated', 'partial', 'allocated', 'void'))
);
CREATE UNIQUE INDEX IF NOT EXISTS billing_payments_no_uq
    ON billing_payments (payment_no);
CREATE INDEX IF NOT EXISTS billing_payments_received_idx
    ON billing_payments (received_on DESC, id DESC);

CREATE TABLE IF NOT EXISTS billing_payment_allocations (
    id BIGSERIAL PRIMARY KEY,
    payment_id BIGINT NOT NULL REFERENCES billing_payments (id) ON DELETE CASCADE,
    order_id BIGINT NOT NULL REFERENCES billing_orders (id) ON DELETE RESTRICT,
    amount NUMERIC(12,2) NOT NULL,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_payment_allocations_amount_check CHECK (amount > 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS billing_payment_allocations_payment_order_uq
    ON billing_payment_allocations (payment_id, order_id);
CREATE INDEX IF NOT EXISTS billing_payment_allocations_order_idx
    ON billing_payment_allocations (order_id);
CREATE INDEX IF NOT EXISTS billing_payment_allocations_payment_idx
    ON billing_payment_allocations (payment_id);
