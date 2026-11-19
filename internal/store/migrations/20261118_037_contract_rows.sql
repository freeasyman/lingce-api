-- 037: 合同行模型（Phase 1 数据层）
-- 目标：subscription_periods 成为服务期唯一事实源；一机构多行；每行记类型。
-- 本迁移为纯新增 + 回填，不改登录行为，可回滚（删列/删行即可）。

-- 1) 合同类型列
ALTER TABLE subscription_periods ADD COLUMN IF NOT EXISTS contract_type text NOT NULL DEFAULT 'formal';

-- 2) 已有行按 source 回填类型（trial → trial，其余默认 formal）
UPDATE subscription_periods SET contract_type = 'trial' WHERE source = 'trial' AND contract_type = 'formal';

-- 3) 主体合同部分唯一索引：同一机构同一时刻最多一条生效的 formal/trial（experience 不受限）
CREATE UNIQUE INDEX IF NOT EXISTS uq_subscription_periods_primary_active
    ON subscription_periods (tenant_id)
    WHERE status = 'active' AND contract_type IN ('formal', 'trial');

-- 4) 回填生效中的主体合同行：当前有效（valid_to >= 今天）但没有合同行的租户。
--    口径与登录一致（COALESCE(valid_to, service_expired_on)），不改变现有登录行为。
INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source, contract_type)
SELECT t.id,
       COALESCE(t.valid_from, t.service_started_on)::date,
       COALESCE(t.valid_to, t.service_expired_on)::date,
       'active',
       CASE WHEN lower(COALESCE(t.account_mode, 'formal')) = 'trial' THEN 'trial' ELSE 'activation' END,
       CASE WHEN lower(COALESCE(t.account_mode, 'formal')) = 'trial' THEN 'trial' ELSE 'formal' END
FROM tenants t
WHERE t.deleted_at IS NULL
  AND t.id > 0
  AND COALESCE(t.valid_to, t.service_expired_on) >= CURRENT_DATE
  AND COALESCE(t.valid_from, t.service_started_on) IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM subscription_periods p WHERE p.tenant_id = t.id);

-- 5) 已过期但有新系统计费记录（tenant_subscriptions 新列）的租户：留一条 expired 审计行。
--    过期行不覆盖今天，不影响登录；保留历史语义（“已过期”而非“从未开通”）。
INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source, contract_type)
SELECT t.id,
       s.started_on,
       s.expired_on,
       'expired',
       CASE WHEN lower(COALESCE(t.account_mode, 'formal')) = 'trial' THEN 'trial' ELSE 'activation' END,
       CASE WHEN lower(COALESCE(t.account_mode, 'formal')) = 'trial' THEN 'trial' ELSE 'formal' END
FROM tenants t
JOIN tenant_subscriptions s ON s.tenant_id = t.id
WHERE t.deleted_at IS NULL
  AND t.id > 0
  AND COALESCE(t.valid_to, t.service_expired_on) < CURRENT_DATE
  AND s.started_on IS NOT NULL
  AND s.expired_on IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM subscription_periods p WHERE p.tenant_id = t.id);

-- 6) 体验窗口迁成 experience 行（不动 tenants 旧字段，双轨期间旧字段照常读）
INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source, contract_type)
SELECT id, new_system_trial_started_at::date, new_system_trial_ends_at::date, 'active', 'manual', 'experience'
FROM tenants
WHERE deleted_at IS NULL
  AND new_system_trial_started_at IS NOT NULL
  AND new_system_trial_ends_at IS NOT NULL
  AND new_system_trial_ends_at >= CURRENT_DATE;

-- 7) 额度挂 period_id（第 2 阶段起扣量按合同行归属）
ALTER TABLE product_entitlements ADD COLUMN IF NOT EXISTS period_id bigint;

-- 7a) 正式额度挂 formal 行，试用额度挂 trial 行
UPDATE product_entitlements e
SET period_id = p.id
FROM subscription_periods p
WHERE p.tenant_id = e.tenant_id
  AND p.status = 'active'
  AND e.period_id IS NULL
  AND (CASE WHEN e.quota_type IN ('official', 'rollover') THEN 'formal' ELSE 'trial' END) = p.contract_type;

-- 7b) 剩余额度退而挂该租户任一 active 行
UPDATE product_entitlements e
SET period_id = p.id
FROM subscription_periods p
WHERE p.tenant_id = e.tenant_id
  AND p.status = 'active'
  AND e.period_id IS NULL;
