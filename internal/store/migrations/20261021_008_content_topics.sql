CREATE TABLE IF NOT EXISTS content_topics (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    title VARCHAR(300) NOT NULL,
    question TEXT NOT NULL DEFAULT '',
    source VARCHAR(50) NOT NULL DEFAULT 'AI 回答',
    source_id BIGINT,
    status VARCHAR(30) NOT NULL DEFAULT 'pending',
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_content_topics_tenant ON content_topics (tenant_id, created_at DESC);
