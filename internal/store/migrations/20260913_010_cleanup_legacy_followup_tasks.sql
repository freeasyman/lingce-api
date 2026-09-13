-- 随访旧任务清理策略调整为非破坏性方案。
--
-- 本迁移故意不删除任何历史任务，避免迁移执行后老租户的任务列表突然变空。
-- 旧 Worker 任务只在同一录音的新随访任务成功生成并入库后，由
-- internal/followup/service.go 按 recording_id 定向清理。
--
-- 清理失败或生成空数组时，旧任务保持不变；completed、cancelled、returned
-- 以及状态不明确的记录始终保留。
--
-- 署名：Codex
-- 时间：2026-09-13

SELECT 1;
