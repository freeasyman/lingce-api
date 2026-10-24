-- 恢复 recording_content_seeds 表。
-- 背景：20261023_003_phase3_drop_old.sql 将其 DROP，但 lingce-worker 的分析链路
-- （doctor / therapist / conversion_parallel）仍在写这张表，导致所有分析回放在
-- "persist content seeds" 步骤报 relation does not exist 而中断。
-- 该表由 worker 写入、内容模块迁移时读取过（此后内容模块改用 content_insights），
-- 恢复它仅为保证 worker 分析链路可用；内容模块不再依赖它。
CREATE TABLE IF NOT EXISTS recording_content_seeds (
    id                  bigserial PRIMARY KEY,
    tenant_id           bigint NOT NULL,
    recording_id        bigint NOT NULL,
    employee_id         bigint,
    seed_type           text,
    topic               text,
    seed_data           jsonb,
    content_angle       text,
    suggested_platforms jsonb,
    viral_potential     text,
    patient_concern     text,
    concern_cluster_id  bigint,
    status              text NOT NULL DEFAULT 'pending',
    used_content_id     bigint,
    created_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_recording_content_seeds_recording_id ON recording_content_seeds(recording_id);
CREATE INDEX IF NOT EXISTS idx_recording_content_seeds_tenant_id ON recording_content_seeds(tenant_id);
