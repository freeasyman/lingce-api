-- Create unified knowledge_items table
-- Replaces: frontdesk_knowledge_bases, frontdesk_prompt_knowledge_mapping, promotion_product_knowledges

BEGIN;

-- ============================================================
-- 1) Create knowledge_items table
-- ============================================================
CREATE TABLE IF NOT EXISTS knowledge_items (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       BIGINT NOT NULL,

    -- 知识归属
    scope           VARCHAR(30) NOT NULL,   -- 'frontdesk', 'doctor', 'consultant', 'common'
    category        VARCHAR(30) NOT NULL,   -- 'service', 'product', 'script', 'compliance', 'temporal', 'faq'

    -- 知识内容
    title           VARCHAR(200) NOT NULL,
    content         TEXT NOT NULL,
    tags            TEXT[] DEFAULT '{}',

    -- 产品关联（仅 category='product' 时使用）
    product_name    VARCHAR(200),

    -- 来源追溯
    source_type     VARCHAR(20) NOT NULL,   -- 'manual', 'recording', 'document'
    source_ref      VARCHAR(200),           -- 'recording:996:event:3', 'upload:1', 'manual:张三'

    -- 生命周期
    status          VARCHAR(10) NOT NULL DEFAULT 'draft',  -- 'draft', 'active', 'archived'
    expires_at      TIMESTAMPTZ,

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 主查询路径：按 tenant + scope 拉取活跃知识
CREATE INDEX idx_ki_tenant_scope ON knowledge_items (tenant_id, scope, status);
-- 按产品筛选
CREATE INDEX idx_ki_product ON knowledge_items (tenant_id, product_name) WHERE product_name IS NOT NULL;
-- 过期清理
CREATE INDEX idx_ki_expires ON knowledge_items (expires_at) WHERE expires_at IS NOT NULL;
-- 草稿审核列表
CREATE INDEX idx_ki_draft ON knowledge_items (tenant_id, status, created_at) WHERE status = 'draft';
-- 来源追溯
CREATE INDEX idx_ki_source ON knowledge_items (source_ref) WHERE source_ref IS NOT NULL;

-- ============================================================
-- 2) Migrate promotion_product_knowledges → knowledge_items
--    Extract product_summary as readable content; deduplicate by tenant+product_name (keep latest)
-- ============================================================
INSERT INTO knowledge_items (tenant_id, scope, category, title, content, tags, product_name, source_type, source_ref, status, created_at, updated_at)
SELECT DISTINCT ON (tenant_id, product_name)
    tenant_id,
    'common',
    'product',
    product_name,
    COALESCE(knowledge_data::jsonb ->> 'product_summary', LEFT(knowledge_data::text, 500)),
    ARRAY[medical_domain],
    product_name,
    'document',
    'migrated:promotion_product_knowledges:' || id,
    'active',
    COALESCE(created_at, NOW()),
    NOW()
FROM promotion_product_knowledges
WHERE status = 'active'
ORDER BY tenant_id, product_name, created_at DESC;

-- ============================================================
-- 3) Mark old tables as deprecated (not dropping yet)
-- ============================================================
COMMENT ON TABLE frontdesk_knowledge_bases IS 'DEPRECATED: replaced by knowledge_items';
COMMENT ON TABLE frontdesk_prompt_knowledge_mapping IS 'DEPRECATED: replaced by knowledge_items';
COMMENT ON TABLE promotion_product_knowledges IS 'DEPRECATED: replaced by knowledge_items';

COMMIT;
