-- 内容获客：AI 答案可见性诊断记录（每次多引擎查询落库，支持历史与复测对比）。
-- 署名：DSH
-- 时间：2026-10-27

CREATE TABLE IF NOT EXISTS content_ai_visibility_runs (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  BIGINT NOT NULL,
    question   TEXT NOT NULL,
    region     TEXT NOT NULL DEFAULT '',
    org_name   TEXT NOT NULL DEFAULT '',
    engines    JSONB NOT NULL DEFAULT '[]'::jsonb,
    gap        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_content_ai_visibility_runs_tenant
    ON content_ai_visibility_runs (tenant_id, created_at DESC);
