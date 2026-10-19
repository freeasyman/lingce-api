-- 患者运营：经营计划 V1
-- 原则：
--   1. products 只增加产品固有属性列，不改现有列、状态枚举与唯一索引；
--   2. 场景、风险、目标属于经营计划/名单/任务，不进入产品表；
--   3. patient_operation_tasks 只做加法扩展，现有任务与接口不受影响。

-- 1. 产品库：产品固有匹配属性
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS applicable_keywords JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS age_min INTEGER,
    ADD COLUMN IF NOT EXISTS age_max INTEGER,
    ADD COLUMN IF NOT EXISTS gender TEXT NOT NULL DEFAULT 'all',
    ADD COLUMN IF NOT EXISTS contraindications TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS review_status TEXT NOT NULL DEFAULT 'approved';

CREATE INDEX IF NOT EXISTS idx_products_applicable_keywords
    ON products USING GIN (applicable_keywords);

CREATE INDEX IF NOT EXISTS idx_products_review_status
    ON products(tenant_id, review_status, status);

-- 2. 经营计划
CREATE TABLE IF NOT EXISTS patient_ops_plans (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    plan_type TEXT NOT NULL CHECK (plan_type IN ('status', 'sales')),
    scenario TEXT NOT NULL DEFAULT '',
    period_start DATE,
    period_end DATE,
    target_count INTEGER,
    target_amount NUMERIC(12, 2),
    product_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    filter_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'closed')),
    created_by BIGINT,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_patient_ops_plans_tenant_status
    ON patient_ops_plans(tenant_id, status, period_start DESC);

-- 3. 计划名单
CREATE TABLE IF NOT EXISTS patient_ops_plan_members (
    id BIGSERIAL PRIMARY KEY,
    plan_id BIGINT NOT NULL REFERENCES patient_ops_plans(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    recommended_product_id BIGINT REFERENCES products(id),
    match_score INTEGER,
    reason TEXT NOT NULL DEFAULT '',
    reason_source TEXT NOT NULL DEFAULT 'fact',
    risk_medical TEXT NOT NULL DEFAULT '',
    risk_commercial TEXT NOT NULL DEFAULT '',
    task_id BIGINT,
    status TEXT NOT NULL DEFAULT 'selected'
        CHECK (status IN ('selected', 'published', 'completed', 'removed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (plan_id, customer_id)
);

CREATE INDEX IF NOT EXISTS idx_patient_ops_plan_members_plan
    ON patient_ops_plan_members(tenant_id, plan_id, status);

-- 4. 待办任务：加法扩展
ALTER TABLE patient_operation_tasks
    ADD COLUMN IF NOT EXISTS plan_id BIGINT,
    ADD COLUMN IF NOT EXISTS plan_member_id BIGINT,
    ADD COLUMN IF NOT EXISTS task_subtype TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS product_id BIGINT,
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 5,
    ADD COLUMN IF NOT EXISTS match_score INTEGER,
    ADD COLUMN IF NOT EXISTS risk_medical TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS risk_commercial TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS next_action TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS next_due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dedupe_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS expected_result TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS result_amount NUMERIC(12, 2),
    ADD COLUMN IF NOT EXISTS result_product_id BIGINT,
    ADD COLUMN IF NOT EXISTS amount_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS created_by BIGINT;

CREATE INDEX IF NOT EXISTS idx_patient_operation_tasks_plan
    ON patient_operation_tasks(tenant_id, plan_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_patient_operation_tasks_dedupe
    ON patient_operation_tasks(tenant_id, dedupe_key)
    WHERE dedupe_key <> '';
