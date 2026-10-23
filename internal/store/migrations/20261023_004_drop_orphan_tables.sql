-- 20261023_004_drop_orphan_tables.sql
-- Phase 3 补充：删除随旧模块一起废弃的表（发布任务 / AI 先答）。

DROP TABLE IF EXISTS content_publish_tasks CASCADE;
DROP TABLE IF EXISTS content_ai_answers CASCADE;
