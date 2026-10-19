-- 补齐 Qwen-Max / Qwen-Plus / Qwen3-VL-Plus / DeepSeek 的缓存单价。
-- 价格单位：元 / 1K tokens。
--
-- Qwen-Max / Qwen-Plus / Qwen3-VL-Plus 适用百炼通用规则：
--   隐式缓存命中 = 输入单价 × 20%
--   显式缓存命中 = 输入单价 × 10%
--   显式缓存创建 = 输入单价 × 125%
--
-- DeepSeek 只做隐式缓存；官方缓存命中价分高峰/空闲，此处先按高峰价落库，
-- 峰谷价后续单独做。deepseek-v4-pro 采用 0.30 元/百万，flash/chat/reasoner
-- 采用 0.04 元/百万。

UPDATE llm_model_configs
SET cached_input_token_price = COALESCE(NULLIF(cached_input_token_price, 0), 0.00048),
    explicit_cached_input_token_price = COALESCE(NULLIF(explicit_cached_input_token_price, 0), 0.00024),
    cache_creation_token_price = COALESCE(NULLIF(cache_creation_token_price, 0), 0.003),
    price_version = COALESCE(price_version, '2026-10-qwen-max-cache'),
    updated_at = NOW()
WHERE model_code = 'qwen-max'
  AND deleted_at IS NULL;

UPDATE llm_model_configs
SET cached_input_token_price = COALESCE(NULLIF(cached_input_token_price, 0), 0.00016),
    explicit_cached_input_token_price = COALESCE(NULLIF(explicit_cached_input_token_price, 0), 0.00008),
    cache_creation_token_price = COALESCE(NULLIF(cache_creation_token_price, 0), 0.001),
    price_version = COALESCE(price_version, '2026-10-qwen-plus-cache'),
    updated_at = NOW()
WHERE model_code = 'qwen-plus'
  AND deleted_at IS NULL;

UPDATE llm_model_configs
SET cached_input_token_price = COALESCE(NULLIF(cached_input_token_price, 0), 0.0002),
    explicit_cached_input_token_price = COALESCE(NULLIF(explicit_cached_input_token_price, 0), 0.0001),
    cache_creation_token_price = COALESCE(NULLIF(cache_creation_token_price, 0), 0.00125),
    price_version = COALESCE(price_version, '2026-10-qwen3-vl-plus-cache'),
    updated_at = NOW()
WHERE model_code = 'qwen3-vl-plus'
  AND deleted_at IS NULL;

UPDATE llm_model_configs
SET cached_input_token_price = COALESCE(NULLIF(cached_input_token_price, 0), 0.0003),
    price_version = COALESCE(price_version, '2026-10-deepseek-v4-pro-cache-peak'),
    updated_at = NOW()
WHERE model_code = 'deepseek-v4-pro'
  AND deleted_at IS NULL;

UPDATE llm_model_configs
SET cached_input_token_price = COALESCE(NULLIF(cached_input_token_price, 0), 0.00004),
    price_version = COALESCE(price_version, '2026-10-deepseek-v4-flash-cache-peak'),
    updated_at = NOW()
WHERE model_code IN ('deepseek-v4-flash', 'deepseek-chat', 'deepseek-reasoner')
  AND deleted_at IS NULL;
