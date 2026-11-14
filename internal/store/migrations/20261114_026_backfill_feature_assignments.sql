-- 026：补齐机构功能组分配
--
-- 订阅套餐已绑定 feature_group_id 的机构，补齐 tenant_feature_assignments；
-- 缺失会导致机构侧菜单鉴权（如 /departments）直接 403。

INSERT INTO tenant_feature_assignments (tenant_id, group_id, created_at, updated_at)
SELECT s.tenant_id, p.feature_group_id, NOW(), NOW()
FROM tenant_subscriptions s
JOIN tenant_subscription_plans p ON p.id = s.plan_id
WHERE p.feature_group_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM tenant_feature_assignments a WHERE a.tenant_id = s.tenant_id
  )
ON CONFLICT (tenant_id) DO NOTHING;
