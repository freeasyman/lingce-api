-- 选题生成异步任务表：提交后立即返回 job_id，后台 goroutine 生成，前端轮询状态。
-- 模式复用 AI 口碑的 run（create → goroutine → poll），表字段贴合选题生成。

CREATE TABLE IF NOT EXISTS content_topic_generation_jobs (
    id                   BIGSERIAL PRIMARY KEY,
    tenant_id            BIGINT NOT NULL,
    frequent_question_id BIGINT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'running',  -- running / done / failed
    progress             INT NOT NULL DEFAULT 0,
    result               JSONB NOT NULL DEFAULT '[]'::jsonb, -- 生成的选题列表 [{id,title,goal,recommendation}]
    error                TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_topic_gen_jobs_tenant
    ON content_topic_generation_jobs (tenant_id, created_at DESC);
