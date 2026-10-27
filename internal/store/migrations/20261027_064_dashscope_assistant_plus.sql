-- 千问 AI 助手切到 qwen-plus：DashScope 原生协议 + enable_source 可返回搜索来源 URL；
-- qwen3.8-max 是多模态模型，text-generation 端点不支持联网搜索（400 url error）。
-- 署名：DSH
-- 时间：2026-10-27

UPDATE llm_model_configs
SET model_code = 'qwen-plus',
    model_name = '通义千问 Plus',
    updated_at = now()
WHERE function_type = 'ai_assistant'
  AND provider = 'dashscope'
  AND deleted_at IS NULL;
