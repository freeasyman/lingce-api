-- 选题生成：模型切 qwen3.8-max（json_schema 结构化输出需要）+ 选题表加 brief/insight_ids。
--
-- 背景：选题层定稿为「创作简报」（16 字段），需结构化输出（response_format=json_schema，
-- 只有 qwen3.8-max 支持）。brief 存完整简报，insight_ids 存来源灵感（替换 evidence_ref 单条）。
--
-- 署名：DSH
-- 时间：2026-10-24

UPDATE llm_model_configs
    SET model_code = 'qwen3.8-max'
    WHERE function_type = 'content_topic' AND provider = 'dashscope';

ALTER TABLE content_topics
    ADD COLUMN IF NOT EXISTS brief jsonb,
    ADD COLUMN IF NOT EXISTS insight_ids jsonb;
