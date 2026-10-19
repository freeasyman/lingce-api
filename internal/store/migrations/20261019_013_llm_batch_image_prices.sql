-- 补齐 Qwen3.8-Max/Flash 的 Batch 价格，新增 Qwen-Image-3.0，并下架 Qwen3-VL-Plus。
-- 价格单位：token 价为元 / 1K tokens；图片价为元 / 张。

ALTER TABLE llm_model_configs
    ADD COLUMN IF NOT EXISTS batch_input_token_price NUMERIC(18, 6),
    ADD COLUMN IF NOT EXISTS batch_output_token_price NUMERIC(18, 6),
    ADD COLUMN IF NOT EXISTS image_input_price_per_image NUMERIC(18, 6),
    ADD COLUMN IF NOT EXISTS image_generation_price_per_image NUMERIC(18, 6);

-- Qwen3.8-Max：Batch File / Batch Chat 均为实时价 5 折
-- 输入 12 -> 6，输出 36 -> 18（元/百万）
UPDATE llm_model_configs
SET batch_input_token_price = COALESCE(NULLIF(batch_input_token_price, 0), 0.006),
    batch_output_token_price = COALESCE(NULLIF(batch_output_token_price, 0), 0.018),
    updated_at = NOW()
WHERE model_code = 'qwen3.8-max'
  AND deleted_at IS NULL;

-- Qwen3.8-Flash：Batch File / Batch Chat 均为实时价 5 折
-- 输入 0.8 -> 0.4，输出 2.7 -> 1.35（元/百万）
UPDATE llm_model_configs
SET batch_input_token_price = COALESCE(NULLIF(batch_input_token_price, 0), 0.0004),
    batch_output_token_price = COALESCE(NULLIF(batch_output_token_price, 0), 0.00135),
    updated_at = NOW()
WHERE model_code = 'qwen3.8-flash'
  AND deleted_at IS NULL;

-- Qwen-Image-3.0：1K/2K 同价，输入 0.02 元/张，生成 0.18 元/张。
INSERT INTO llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params,
    image_input_price_per_image, image_generation_price_per_image,
    is_default, is_active, description, created_by, created_at, updated_at
)
SELECT
    0,
    'qwen-image-3.0',
    'image_generation',
    'Qwen-Image-3.0',
    'dashscope',
    '{}'::json,
    '{}'::json,
    0.02,
    0.18,
    false,
    true,
    'Qwen-Image-3.0 图像生成｜1K/2K 同价：输入 0.02 元/张，生成 0.18 元/张',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1 FROM llm_model_configs
    WHERE model_code = 'qwen-image-3.0'
      AND function_type = 'image_generation'
      AND deleted_at IS NULL
);

-- Qwen3-VL-Plus 已下架：停用配置，保留历史记录和价格。
UPDATE llm_model_configs
SET is_active = false,
    description = '已下架（2026-10），保留历史记录',
    updated_at = NOW()
WHERE model_code = 'qwen3-vl-plus'
  AND deleted_at IS NULL;
