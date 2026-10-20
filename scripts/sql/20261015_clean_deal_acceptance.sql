-- 赢客专家验收数据清理：只删除验收租户 9001 的固定 ID 区间。
-- 保留租户、员工和角色，避免影响其他验收（阶段 0 账号可复用）。
BEGIN;

DELETE FROM recordings WHERE tenant_id = 9001 AND id BETWEEN 7001 AND 7008;
DELETE FROM encounters WHERE tenant_id = 9001 AND id BETWEEN 8001 AND 8008;
DELETE FROM customers WHERE tenant_id = 9001 AND id BETWEEN 6001 AND 6008;

COMMIT;
