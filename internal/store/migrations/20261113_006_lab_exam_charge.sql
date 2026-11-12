-- 检验/检查开单自动挂收费单：申请单记录所选收费项目、金额快照与生成的收费单。
-- 只给 his_lab_requests / his_exam_requests 加列与索引，不改共享表（his_orders/his_order_items 复用既有结构）。
-- charge_amount 为下单时的价格快照；未关联收费项目时保持 0 且 order_id 为空（允许开单但不产生收费单）。

ALTER TABLE his_lab_requests
    ADD COLUMN IF NOT EXISTS charge_item_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS charge_amount NUMERIC(10,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS order_id BIGINT NULL;

ALTER TABLE his_exam_requests
    ADD COLUMN IF NOT EXISTS charge_item_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS charge_amount NUMERIC(10,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS order_id BIGINT NULL;

-- 收费单反查申请单（收费页/对账排查用）。
CREATE INDEX IF NOT EXISTS idx_his_lab_requests_order
    ON his_lab_requests(order_id);
CREATE INDEX IF NOT EXISTS idx_his_exam_requests_order
    ON his_exam_requests(order_id);

