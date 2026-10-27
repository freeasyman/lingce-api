-- AI 可见性诊断历史支持软删除
-- 真删会丢失：生成内容的来源依据、成本审计对照。软删除对客户等价于删除。
-- 署名：DSH
-- 时间：2026-10-27

ALTER TABLE content_ai_visibility_runs
    ADD COLUMN IF NOT EXISTS deleted_at timestamptz;

CREATE INDEX IF NOT EXISTS idx_ai_visibility_runs_active
    ON content_ai_visibility_runs (tenant_id, created_at DESC)
    WHERE deleted_at IS NULL;
