-- 计费运营端第 2 批：暂停服务 / 取消服务 / 作废订单。
--
-- 状态落库口径（MVP）：
--   · 暂停 = tenants.paused_at 非空（paused_reason 记录原因）；恢复 = 清空两列。
--     暂停只影响扣量闸门，不改服务期、不改额度、不顺延到期日。
--   · 取消 = tenants.cancelled_at 非空（cancelled_reason 记录原因），同时把当前服务期
--     置 terminated、tenant_subscriptions 同步 terminated，额度行归零并写 expired 流水。
--     取消不可逆，cancelled_at 一旦写入不再清除。
--
-- 订单新增 void 状态：只允许作废「未收款且未核销」的订单。
-- 迁移可重复执行（IF NOT EXISTS / 先 DROP 再 ADD 约束）。

ALTER TABLE IF EXISTS tenants
    ADD COLUMN IF NOT EXISTS paused_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS paused_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cancelled_reason TEXT NOT NULL DEFAULT '';

-- 暂停/取消状态查询是机构详情与扣量闸门的热路径，用部分索引只覆盖真正处于该状态的行。
CREATE INDEX IF NOT EXISTS tenants_paused_idx
    ON tenants (paused_at) WHERE paused_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS tenants_cancelled_idx
    ON tenants (cancelled_at) WHERE cancelled_at IS NOT NULL;

-- billing_orders.status 增加 void（作废）。历史 CHECK 里没有 void，先删后加，
-- 重复执行时 DROP IF EXISTS 是 no-op。
ALTER TABLE IF EXISTS billing_orders
    DROP CONSTRAINT IF EXISTS billing_orders_status_check;
ALTER TABLE IF EXISTS billing_orders
    ADD CONSTRAINT billing_orders_status_check
    CHECK (status IN ('created', 'paid', 'settled', 'refunded', 'cancelled', 'void'));
