-- LLM 模型计费维度扩展：缓存命中、显式缓存创建、音频按秒。
-- 价格单位与既有 input_token_price/output_token_price 保持一致：元 / 1K tokens。
-- 官方价格页通常以“元 / 百万 tokens”展示，写入时需要除以 1000。

ALTER TABLE llm_model_configs
    ADD COLUMN IF NOT EXISTS cached_input_token_price NUMERIC(18, 6),
    ADD COLUMN IF NOT EXISTS explicit_cached_input_token_price NUMERIC(18, 6),
    ADD COLUMN IF NOT EXISTS cache_creation_token_price NUMERIC(18, 6),
    ADD COLUMN IF NOT EXISTS audio_price_per_second NUMERIC(18, 8),
    ADD COLUMN IF NOT EXISTS price_currency TEXT NOT NULL DEFAULT 'CNY',
    ADD COLUMN IF NOT EXISTS price_version TEXT,
    ADD COLUMN IF NOT EXISTS price_effective_from TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Qwen3.8-Max: 输入 12 / 缓存命中 1.5 / 显式缓存命中 1 / 缓存创建 15 / 输出 36（元/百万）
UPDATE llm_model_configs
SET input_token_price = COALESCE(NULLIF(input_token_price, 0), 0.012),
    output_token_price = COALESCE(NULLIF(output_token_price, 0), 0.036),
    cached_input_token_price = COALESCE(cached_input_token_price, 0.0015),
    explicit_cached_input_token_price = COALESCE(explicit_cached_input_token_price, 0.001),
    cache_creation_token_price = COALESCE(cache_creation_token_price, 0.015),
    price_version = COALESCE(price_version, '2026-10-qwen3.8-max'),
    price_effective_from = COALESCE(price_effective_from, NOW()),
    updated_at = NOW()
WHERE model_code = 'qwen3.8-max'
  AND deleted_at IS NULL;

-- Qwen3.8-Flash: 输入 0.8 / 缓存命中 0.1 / 显式缓存命中 0.1 / 缓存创建 1.25 / 输出 2.7（元/百万）
UPDATE llm_model_configs
SET input_token_price = COALESCE(NULLIF(input_token_price, 0), 0.0008),
    output_token_price = COALESCE(NULLIF(output_token_price, 0), 0.0027),
    cached_input_token_price = COALESCE(cached_input_token_price, 0.0001),
    explicit_cached_input_token_price = COALESCE(explicit_cached_input_token_price, 0.0001),
    cache_creation_token_price = COALESCE(cache_creation_token_price, 0.00125),
    price_version = COALESCE(price_version, '2026-10-qwen3.8-flash'),
    price_effective_from = COALESCE(price_effective_from, NOW()),
    updated_at = NOW()
WHERE model_code = 'qwen3.8-flash'
  AND deleted_at IS NULL;

-- 其余在用的文本模型先补齐基础输入/输出价，避免模型配置页继续显示 0。
-- 缓存价暂缺的模型由网关回退到输入价，并在审计里标记 pricing_missing。
UPDATE llm_model_configs AS c
SET input_token_price = v.input_price,
    output_token_price = v.output_price,
    price_version = COALESCE(c.price_version, '2026-10-static-fallback'),
    updated_at = NOW()
FROM (
    VALUES
        ('qwen-max', 0.0024, 0.0096),
        ('qwen-plus', 0.0008, 0.0020),
        ('qwen-turbo', 0.0003, 0.0006),
        ('qwen3-max', 0.0025, 0.0100),
        ('qwen3-vl-plus', 0.0010, 0.0100),
        ('deepseek-chat', 0.0010, 0.0020),
        ('deepseek-reasoner', 0.0010, 0.0020),
        ('deepseek-v4-flash', 0.0010, 0.0020),
        ('deepseek-v4-pro', 0.0030, 0.0060)
) AS v(model_code, input_price, output_price)
WHERE c.model_code = v.model_code
  AND c.deleted_at IS NULL
  AND (c.input_token_price IS NULL OR c.input_token_price = 0)
  AND (c.output_token_price IS NULL OR c.output_token_price = 0);

-- 语音识别按秒计价，补齐现有 ASR 配置。
UPDATE llm_model_configs
SET audio_price_per_second = 0.00022,
    price_version = COALESCE(price_version, '2026-10-audio-fun-asr'),
    updated_at = NOW()
WHERE model_code = 'fun-asr'
  AND deleted_at IS NULL
  AND audio_price_per_second IS NULL;

UPDATE llm_model_configs
SET audio_price_per_second = 0.00024,
    price_version = COALESCE(price_version, '2026-10-audio-paraformer-realtime'),
    updated_at = NOW()
WHERE model_code = 'paraformer-realtime-v2'
  AND deleted_at IS NULL
  AND audio_price_per_second IS NULL;

UPDATE llm_model_configs
SET audio_price_per_second = 0.00008,
    price_version = COALESCE(price_version, '2026-10-audio-paraformer'),
    updated_at = NOW()
WHERE model_code = 'paraformer-v2'
  AND deleted_at IS NULL
  AND audio_price_per_second IS NULL;
