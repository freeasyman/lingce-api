-- Immutable snapshots for content editing/regeneration/rollback.
CREATE TABLE IF NOT EXISTS content_item_versions (
    id BIGSERIAL PRIMARY KEY,
    content_id BIGINT NOT NULL REFERENCES content_items(id) ON DELETE CASCADE,
    tenant_id BIGINT NOT NULL,
    version INTEGER NOT NULL,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    content_type VARCHAR(50),
    platform VARCHAR(50),
    status VARCHAR(30) NOT NULL,
    extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (content_id, version)
);
CREATE INDEX IF NOT EXISTS idx_content_item_versions_content
  ON content_item_versions (content_id, version DESC);
