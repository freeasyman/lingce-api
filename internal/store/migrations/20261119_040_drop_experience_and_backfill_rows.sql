-- 040: 收敛到「新产品一套合同」。
-- 1) 体验概念删除：体验窗口并入试用行，体验行全部删除。
-- 2) 删除没有额度、没有订单的合同行（回填残留/历史行）：
--    老产品订阅数据在 tenants + tenant_subscriptions，不在本表。
-- 3) 额度挂回各自合同行（period_id）。

-- 1a) 把进行中的体验行到期日并入同租户的试用行
UPDATE subscription_periods p
SET expired_on = e.expired_on, updated_at = NOW()
FROM subscription_periods e
WHERE e.tenant_id = p.tenant_id
  AND e.contract_type = 'experience'
  AND p.contract_type = 'trial'
  AND p.status = 'active';

-- 1b) 删除全部体验行
DELETE FROM subscription_periods WHERE contract_type = 'experience';

-- 2) 删除无额度、无订单的合同行（老数据不动，只清理新产品合同表）
DELETE FROM subscription_periods p
WHERE NOT EXISTS (SELECT 1 FROM product_entitlements e WHERE e.tenant_id = p.tenant_id)
  AND NOT EXISTS (SELECT 1 FROM billing_orders o WHERE o.tenant_id = p.tenant_id);

-- 3) 剩余额度行挂到所属合同行
UPDATE product_entitlements e
SET period_id = p.id
FROM subscription_periods p
WHERE p.tenant_id = e.tenant_id
  AND p.status = 'active'
  AND e.period_id IS NULL;
