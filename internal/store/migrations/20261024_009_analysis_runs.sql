-- 录音→洞察→选题 的批次台账（任务化记录），支持进度/历史/重试。
CREATE TABLE IF NOT EXISTS content_analysis_runs (
    id           bigserial PRIMARY KEY,
    tenant_id    bigint NOT NULL,
    trigger      text NOT NULL,               -- worker | manual
    status       text NOT NULL DEFAULT 'running', -- running | succeeded | partial | failed
    total        int  NOT NULL DEFAULT 0,
    done         int  NOT NULL DEFAULT 0,
    failed       int  NOT NULL DEFAULT 0,
    insights     int  NOT NULL DEFAULT 0,
    topics       int  NOT NULL DEFAULT 0,
    error        text,
    started_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz
);

CREATE INDEX IF NOT EXISTS idx_content_analysis_runs_tenant_started
    ON content_analysis_runs (tenant_id, started_at DESC);
