-- 数据修正：取消全局模板数据，将 tenant_id = 0 的产品归入 tenant_id = 1。
-- 仅数据变更，不改表结构；is_template 保持原值。
--
-- 兼容迁移 seedTenantProductCatalog 已改为写入 tenant_id = 1，不再创建 tenant 0 产品。
-- 本脚本仅用于仍残留 tenant_id = 0 产品的历史库；全新库初始化后执行本脚本为空操作。
UPDATE products
SET tenant_id = 1,
    updated_at = NOW()
WHERE tenant_id = 0;
