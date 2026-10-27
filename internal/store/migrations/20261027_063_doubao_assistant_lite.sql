-- 豆包 AI 助手切到 Seed 2.1 Lite：pro 长上下文要 115s（接近超时），lite 约 47s。
-- 署名：DSH
-- 时间：2026-10-27

UPDATE llm_model_configs
SET model_code = 'doubao-seed-2-1-lite-260915',
    model_name = '豆包 Seed 2.1 Lite',
    updated_at = now()
WHERE function_type = 'ai_assistant'
  AND provider = 'doubao'
  AND deleted_at IS NULL;
