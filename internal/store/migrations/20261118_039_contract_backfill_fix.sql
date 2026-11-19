-- 039: 修正 037 回填过宽的问题。
-- 规则（与业务确认一致）：只有真正开通过（有额度或订单）的租户才保留合同；
-- 没有额度的租户回到「未开通」；订阅已过期的改为「已过期」。
-- 本迁移对已应用 037 的库做纠正，对全新库同样幂等（037 先回填、039 再收敛）。

-- 1) 订阅已过期：active 合同行改为 expired（保留历史，不再生效）
UPDATE subscription_periods p
SET status = 'expired', updated_at = NOW()
WHERE p.status = 'active'
  AND p.contract_type IN ('formal', 'trial')
  AND EXISTS (
      SELECT 1 FROM tenant_subscriptions s
      WHERE s.tenant_id = p.tenant_id AND s.expired_on IS NOT NULL AND s.expired_on < CURRENT_DATE
  );

-- 2) 从未开通（无额度、无订单、订阅未过期）：删除回填出来的 active 行 → 未开通
DELETE FROM subscription_periods p
WHERE p.status = 'active'
  AND p.contract_type IN ('formal', 'trial')
  AND NOT EXISTS (SELECT 1 FROM product_entitlements e WHERE e.tenant_id = p.tenant_id)
  AND NOT EXISTS (SELECT 1 FROM billing_orders o WHERE o.tenant_id = p.tenant_id)
  AND NOT EXISTS (
      SELECT 1 FROM tenant_subscriptions s
      WHERE s.tenant_id = p.tenant_id AND s.expired_on IS NOT NULL AND s.expired_on < CURRENT_DATE
  );

-- 3) 反向兜底：有额度但没有任何合同行的租户，补一条主体合同（按有效期日期）
INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source, contract_type)
SELECT t.id,
       COALESCE(t.valid_from, t.service_started_on)::date,
       COALESCE(t.valid_to, t.service_expired_on)::date,
       'active',
       'manual',
       CASE WHEN lower(COALESCE(t.account_mode, 'formal')) = 'trial' THEN 'trial' ELSE 'formal' END
FROM tenants t
WHERE t.deleted_at IS NULL
  AND t.id > 0
  AND COALESCE(t.valid_to, t.service_expired_on) >= CURRENT_DATE
  AND COALESCE(t.valid_from, t.service_started_on) IS NOT NULL
  AND EXISTS (SELECT 1 FROM product_entitlements e WHERE e.tenant_id = t.id)
  AND NOT EXISTS (SELECT 1 FROM subscription_periods p WHERE p.tenant_id = t.id);

-- 4) 机构缓存有效期与订阅对齐（订阅已过期的，tenants.valid_to 不再显示未来日期）
UPDATE tenants t
SET valid_to = s.expired_on::timestamp, updated_at = NOW()
FROM tenant_subscriptions s
WHERE s.tenant_id = t.id
  AND s.expired_on IS NOT NULL
  AND s.expired_on < CURRENT_DATE
  AND t.valid_to IS NOT NULL
  AND t.valid_to::date <> s.expired_on;
