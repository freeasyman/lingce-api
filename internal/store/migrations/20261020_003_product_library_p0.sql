-- 产品库 P0：软删除、版本号、catalog 支撑。
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS version INT NOT NULL DEFAULT 1;

-- 同名产品只在未删除范围内唯一，允许删除后重建。
DROP INDEX IF EXISTS uk_products_tenant_name_template;
CREATE UNIQUE INDEX IF NOT EXISTS uk_products_tenant_name_template
    ON products (tenant_id, lower(name), is_template)
    WHERE deleted_at IS NULL;

-- catalog 固定 active + approved + 非模板 + 未删除。
CREATE INDEX IF NOT EXISTS idx_products_catalog
    ON products (tenant_id, status, review_status, is_template, deleted_at);
