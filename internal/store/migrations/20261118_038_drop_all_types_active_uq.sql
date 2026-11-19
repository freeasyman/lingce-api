-- 038: 放开「一机构多行生效合同」——删除旧的全体 active 唯一索引。
-- 合同行模型要求正式/试用与体验可以并行生效（formal active + experience active 并存），
-- 而历史索引 subscription_periods_active_uq 按 (tenant_id) WHERE status='active'
-- 限制所有类型只能有一条 active，会拒绝体验行插入。
-- 主体唯一性已由 037 的部分唯一索引 uq_subscription_periods_primary_active
-- （仅 formal/trial）承接，因此这里安全删除旧索引。
DROP INDEX IF EXISTS subscription_periods_active_uq;
