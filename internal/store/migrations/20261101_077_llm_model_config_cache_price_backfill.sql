-- 缓存价回填：迁移 076 只写 input/output 价、漏写 cached 列，导致 qwen3.8-flash /
-- qwen3.8-max 新增行及切换行的 cached_input_token_price 为 NULL。网关按 model_code
-- 全局取 updated_at 最新一行，NULL 被 COALESCE 成 0，再回退主输入价，flash 缓存 token
-- 被按 0.0008 全价计（应 0.0001）。这里把 active + tenant 0 + cached 为 NULL 的行回填到
-- model_pricing.json 的官方缓存价，保证新环境从零部署也有缓存价。
--
-- 幂等约束：只回填 cached_input_token_price IS NULL 的行，不覆盖已有（正确）值；
-- 重复执行命中 0 行。仅改缓存价三列，不碰其它价格/模型选择。
--
-- 署名：灵策项目模型配置统一（缓存价回填）
-- 时间：2026-11-01

-- qwen3.8-flash 官方缓存价：cached 0.0001 / explicit 0.0001 / creation 0.00125
UPDATE llm_model_configs
SET cached_input_token_price          = 0.0001,
    explicit_cached_input_token_price = 0.0001,
    cache_creation_token_price        = 0.00125,
    updated_at                        = NOW()
WHERE tenant_id = 0
  AND model_code = 'qwen3.8-flash'
  AND COALESCE(is_active, false) = true
  AND deleted_at IS NULL
  AND cached_input_token_price IS NULL;

-- qwen3.8-max 官方缓存价：cached 0.0015 / explicit 0.001 / creation 0.015
UPDATE llm_model_configs
SET cached_input_token_price          = 0.0015,
    explicit_cached_input_token_price = 0.001,
    cache_creation_token_price        = 0.015,
    updated_at                        = NOW()
WHERE tenant_id = 0
  AND model_code = 'qwen3.8-max'
  AND COALESCE(is_active, false) = true
  AND deleted_at IS NULL
  AND cached_input_token_price IS NULL;
