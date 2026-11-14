-- 034：租户主表有效期与当前有效订阅同步
--
-- 背景：billing 订阅表 tenant_subscriptions 已生效，但 tenants.valid_to 仍是
-- 旧试用期，导致登录被 TENANT_INACTIVE 拒绝。
-- 只同步有 active 且未到期订阅的租户；不修改账号、角色、密码。

WITH latest AS (
    SELECT DISTINCT ON (tenant_id)
        tenant_id,
        started_on,
        expired_on
    FROM tenant_subscriptions
    WHERE status = 'active'
      AND expired_on >= CURRENT_DATE
    ORDER BY tenant_id, expired_on DESC, id DESC
)
UPDATE tenants t
SET valid_from = COALESCE(l.started_on::timestamp, t.valid_from),
    valid_to = (l.expired_on::timestamp + INTERVAL '1 day' - INTERVAL '1 second'),
    service_started_on = l.started_on,
    service_expired_on = l.expired_on,
    updated_at = NOW()
FROM latest l
WHERE t.id = l.tenant_id
  AND t.deleted_at IS NULL;
