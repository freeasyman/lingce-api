-- 年龄未知可以进入候选；是否必须补全年龄由产品明确配置。
-- 现有带年龄区间的产品保持严格规则，不推断产品分类。
ALTER TABLE products ADD COLUMN IF NOT EXISTS age_required BOOLEAN;
UPDATE products SET age_required = (age_min IS NOT NULL OR age_max IS NOT NULL)
WHERE age_required IS NULL;
ALTER TABLE products ALTER COLUMN age_required SET DEFAULT false;
ALTER TABLE products ALTER COLUMN age_required SET NOT NULL;

-- 人工确认只对本次计划、客户和产品版本生效，与名单/任务同事务保存。
CREATE TABLE IF NOT EXISTS patient_ops_product_suitability_reviews (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    plan_id BIGINT NOT NULL REFERENCES patient_ops_plans(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    product_id BIGINT NOT NULL REFERENCES products(id),
    product_version INT NOT NULL,
    operator_id BIGINT NOT NULL,
    reason TEXT NOT NULL CHECK (length(btrim(reason)) > 0),
    customer_age INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_plan_product_suitability_reviews
    ON patient_ops_product_suitability_reviews(tenant_id, plan_id, customer_id);
