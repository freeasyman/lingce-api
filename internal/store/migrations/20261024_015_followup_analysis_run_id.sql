-- 随访生成幂等键：从“一条录音只能生成一次”改为“一次分析 run 生成一次”。
--
-- 背景：旧实现按 tenant_id + recording_id 去重，导致同一录音重新分析后
-- 永远不会再生成任务；而重复回调又需要幂等。这里引入 Worker 的分析 run ID，
-- 同一 run 的重复投递只处理一次，新的 run 允许重新生成。
--
-- 署名：Codex
-- 时间：2026-10-24

ALTER TABLE followup_generation_runs
    ADD COLUMN IF NOT EXISTS analysis_run_id VARCHAR(128);

CREATE UNIQUE INDEX IF NOT EXISTS idx_followup_generation_runs_analysis_run
    ON followup_generation_runs (tenant_id, analysis_run_id)
    WHERE analysis_run_id IS NOT NULL;

COMMENT ON COLUMN followup_generation_runs.analysis_run_id IS
    'Worker 分析 run ID；同一 run 的重复回调幂等，新的 run 允许重新生成随访任务。';
