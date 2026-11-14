-- 032：修复历史机构管理员 is_active 为 NULL
--
-- 早期建租户时 createDefaultTenantAdminTx 没写 is_active，导致：
--   · 账号列表显示「停用」
--   · 产品管理员选择器把它过滤掉
-- 注意：employees.is_active 是 integer 列（历史原因），这里写 1。
UPDATE employees SET is_active = 1, updated_at = NOW() WHERE is_active IS NULL;
