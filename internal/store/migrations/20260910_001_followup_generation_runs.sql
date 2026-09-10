-- 随访生成业务处理记录与失败补偿表。
--
-- 开发库可能已经由人工执行过建表 SQL，因此本迁移全部使用幂等语句：
-- 已存在的表、字段和索引不会被重复创建。API 部署到其他环境时，
-- schema migration runner 会自动执行本文件，确保补偿逻辑所需数据库对象存在。
--
-- 署名：Codex
-- 时间：2026-09-10

CREATE TABLE IF NOT EXISTS followup_generation_runs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id INTEGER NOT NULL,
    recording_id INTEGER NOT NULL,
    encounter_id INTEGER NOT NULL,
    employee_id INTEGER NOT NULL,
    customer_id INTEGER NOT NULL,
    customer_name VARCHAR(255),
    doctor_name VARCHAR(255),
    recorded_at TIMESTAMP WITHOUT TIME ZONE,
    request_id VARCHAR(128) NOT NULL,
    request_payload JSONB NOT NULL,
    prompt_code VARCHAR(128),
    prompt_version VARCHAR(64),
    generation_model VARCHAR(128),
    llm_request_id VARCHAR(128),
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_retry_at TIMESTAMP WITHOUT TIME ZONE,
    last_error_code VARCHAR(128),
    last_error_message TEXT,
    llm_response_payload JSONB,
    generated_tasks_payload JSONB,
    persisted_task_count INTEGER NOT NULL DEFAULT 0,
    started_at TIMESTAMP WITHOUT TIME ZONE,
    completed_at TIMESTAMP WITHOUT TIME ZONE,
    failed_at TIMESTAMP WITHOUT TIME ZONE,
    created_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITHOUT TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_followup_generation_runs_request_id
    ON followup_generation_runs (request_id);

CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_tenant
    ON followup_generation_runs (tenant_id);

CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_recording
    ON followup_generation_runs (recording_id);

CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_encounter
    ON followup_generation_runs (encounter_id);

CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_status_retry
    ON followup_generation_runs (status, next_retry_at);

CREATE INDEX IF NOT EXISTS idx_followup_generation_runs_created_at
    ON followup_generation_runs (created_at DESC);
