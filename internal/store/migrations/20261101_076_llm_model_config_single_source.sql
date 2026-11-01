-- 模型配置单一真相源：补齐自动建库缺失的模型配置，并把视觉/多模态模型统一到 qwen3.8-flash。
--
-- 背景（问题清单 07「通用硬伤」）：
--   1) task_script_generation（随访任务生成）、deal_expert_analysis（赢客分析）此前只在手工脚本，
--      代码自动建库不会建，新环境从零部署就报「模型配置错误」。
--   2) 获客先锋「学视觉风格 / 图文明验」依赖 function_type=image_analysis / image_understanding，
--      此前没有任何 seed，功能点开就报错。
--   3) patient_screenshot_analysis / image_analysis / image_understanding / vision_analysis 三套库分叉，
--      部分环境仍指向已下架模型 qwen3-vl-plus。
--   4) content_insight（内容洞察/灵感）深度分析应统一 qwen3.8-max；dev 曾误配成 qwen3.8-flash
--      （且沿用 max 的价格 0.012/0.036，模型与价格错配）。
--   5) content_layout_extract / content_plan / transcription_cleaning 三行此前只在 ECS test 手工 seed
--      （2026-10-29），dev / RDS test 缺失，代码靠 content_generation / content_topic / chat 兜底，
--      需补进单一真相源，三库显式一致。
--
-- 幂等约束：所有 INSERT 带 WHERE NOT EXISTS；UPDATE 只改目标行；已下架（is_active=false）的历史行保留不删。
-- 只改模型配置表，不碰业务数据。
--
-- 署名：灵策项目模型配置统一
-- 时间：2026-11-01

-- 1) 已下架模型 qwen3-vl-plus 的 active 行切换到 qwen3.8-flash（历史 inactive 行保留）。
UPDATE llm_model_configs
SET model_code = 'qwen3.8-flash',
    model_name = '通义千问 3.8 Flash',
    input_token_price = 0.0008,
    output_token_price = 0.0027,
    price_version = '2026-11-qwen3.8-flash',
    updated_at = NOW()
WHERE tenant_id = 0
  AND model_code = 'qwen3-vl-plus'
  AND function_type IN ('patient_screenshot_analysis', 'image_analysis', 'image_understanding', 'vision_analysis')
  AND COALESCE(is_active, false) = true
  AND deleted_at IS NULL;

-- 2) 视觉/多模态模型：确保每类 function_type 都有 active 的 qwen3.8-flash。
INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type,
  is_default, is_active, model_params, extra_params,
  input_token_price, output_token_price, price_currency, price_version,
  description, created_by, created_at, updated_at
)
SELECT 0, 'qwen3.8-flash', '通义千问 3.8 Flash', 'dashscope', f.function_type,
       true, true, '{"temperature":0.2,"max_tokens":2200,"timeout_seconds":45}'::json, '{}'::json,
       0.0008, 0.0027, 'CNY', '2026-11-qwen3.8-flash',
       '多模态视觉理解（获客先锋学视觉风格/图文明验 + 患者截图）', 0, NOW(), NOW()
FROM (VALUES
  ('patient_screenshot_analysis'),
  ('image_analysis'),
  ('image_understanding'),
  ('vision_analysis')
) AS f(function_type)
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = 0 AND c.function_type = f.function_type
    AND c.model_code = 'qwen3.8-flash' AND c.deleted_at IS NULL
);

-- 3) 随访顾问任务生成模型（此前只在手工脚本；flash 作为唯一默认，避免与 max 双默认歧义）。
INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type,
  is_default, is_active, model_params, extra_params,
  input_token_price, output_token_price, price_currency, price_version,
  description, created_by, created_at, updated_at
)
SELECT 0, 'qwen3.8-flash', '通义千问 3.8 Flash', 'dashscope', 'task_script_generation',
       true, true, '{"temperature":0.2,"max_tokens":1800,"timeout_seconds":60}'::json, '{}'::json,
       0.0008, 0.0027, 'CNY', '2026-10-qwen3.8-flash',
       '随访顾问任务生成模型', 0, NOW(), NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = 0 AND c.function_type = 'task_script_generation'
    AND c.model_code = 'qwen3.8-flash' AND c.deleted_at IS NULL
);

-- 随访任务生成：qwen3.8-max 仅作对照，不再设为默认，确保 resolve 命中 flash。
UPDATE llm_model_configs
SET is_default = false,
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'task_script_generation'
  AND model_code = 'qwen3.8-max'
  AND COALESCE(is_default, false) = true
  AND deleted_at IS NULL;

-- 4) 赢客专家分析模型（此前只在 scripts/sql/20261020_deal_expert_model_config.sql 手工脚本）。
INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type,
  is_default, is_active, model_params, extra_params,
  input_token_price, output_token_price, price_currency, price_version,
  description, created_by, created_at, updated_at
)
SELECT 0, 'qwen3.8-max', '通义千问 3.8 Max', 'dashscope', 'deal_expert_analysis',
       true, true, '{"temperature":0.1,"max_tokens":8192,"response_format":"json","timeout_seconds":180}'::json, '{}'::json,
       0.0120, 0.0360, 'CNY', '2026-10-qwen3.8-max',
       '赢客专家接待分析主模型', 0, NOW(), NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = 0 AND c.function_type = 'deal_expert_analysis'
    AND c.model_code = 'qwen3.8-max' AND c.deleted_at IS NULL
);

INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type,
  is_default, is_active, model_params, extra_params,
  input_token_price, output_token_price, price_currency, price_version,
  description, created_by, created_at, updated_at
)
SELECT 0, 'qwen3.8-flash', '通义千问 3.8 Flash', 'dashscope', 'deal_expert_analysis',
       false, true, '{"temperature":0.1,"max_tokens":8192,"response_format":"json","timeout_seconds":180}'::json, '{}'::json,
       0.0008, 0.0027, 'CNY', '2026-10-qwen3.8-flash',
       '赢客专家分析对照模型：仅回填/A-B，不设为默认', 0, NOW(), NOW()
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = 0 AND c.function_type = 'deal_expert_analysis'
    AND c.model_code = 'qwen3.8-flash' AND c.deleted_at IS NULL
);

-- 5) 价格错漏修正（llm_model_configs 全局 tenant 0）。

-- 5a) content_topic：模型已切 qwen3.8-max，但价格/名称/价格版本仍是 flash 的 → 改为 max。
UPDATE llm_model_configs
SET model_name = '通义千问 3.8 Max',
    input_token_price = 0.012,
    output_token_price = 0.036,
    price_version = '2026-10-qwen3.8-max',
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'content_topic'
  AND model_code = 'qwen3.8-max'
  AND deleted_at IS NULL
  AND (input_token_price IS DISTINCT FROM 0.012
       OR output_token_price IS DISTINCT FROM 0.036
       OR model_name IS DISTINCT FROM '通义千问 3.8 Max'
       OR price_version IS DISTINCT FROM '2026-10-qwen3.8-max');

-- 5b) doubao-seed-2-1-lite：误用了 pro 官方价（price_version=…-pro-list）→ 改为 lite 官方价。
UPDATE llm_model_configs
SET input_token_price = 0.0008,
    output_token_price = 0.0027,
    price_version = '2026-11-doubao-seed-2-1-lite',
    updated_at = NOW()
WHERE tenant_id = 0
  AND model_code = 'doubao-seed-2-1-lite-260915'
  AND deleted_at IS NULL
  AND (input_token_price IS DISTINCT FROM 0.0008
       OR output_token_price IS DISTINCT FROM 0.0027
       OR price_version IS DISTINCT FROM '2026-11-doubao-seed-2-1-lite');

-- 6) content_insight（内容洞察/灵感，从录音提炼内容灵感）是复杂分析，统一 qwen3.8-max。
--    修正历史错配：dev 把 model_code 改成了 qwen3.8-flash，却沿用 max 的价格（0.012/0.036）与名称。
--    RDS test / ECS test 已是 qwen3.8-max；本 UPDATE 幂等（翻转后不再命中 flash 行）。
UPDATE llm_model_configs
SET model_code = 'qwen3.8-max',
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'content_insight'
  AND model_code = 'qwen3.8-flash'
  AND COALESCE(is_active, true) = true
  AND deleted_at IS NULL;

-- 7) 补齐 ECS test 独有、代码自动建库缺失的 content 模型配置（单一真相源）。
--    这三个 function_type 此前只在 ECS test 手工 seed（2026-10-29），dev / RDS test 缺失：
--      content_layout_extract（从公众号 HTML 提取排版 DNA）→ 复杂分析，max；
--      content_plan（月度计划说明）→ 复杂分析，max；
--      transcription_cleaning（转写清洗与说话人识别）→ 轻量清洗，flash。
--    代码 resolveLLMModelConfig 候选依次为
--      content_layout_extract/content_plan 自身 → content_generation/content_topic → chat，
--    缺失时兜底到 content_generation(content_topic)=max，行为虽一致，但显式行三库不齐；
--    这里 INSERT 补齐，并沿用 ECS 手工 seed 的模型/价格/price_version，保证三库逐字段一致。
INSERT INTO llm_model_configs (
  tenant_id, model_code, model_name, provider, function_type,
  is_default, is_active, model_params, extra_params,
  input_token_price, output_token_price, price_currency, price_version,
  description, created_by, created_at, updated_at
)
SELECT 0, v.model_code, v.model_name, 'dashscope', v.function_type,
       true, true, v.model_params::json, '{}'::json,
       v.input_token_price, v.output_token_price, 'CNY', v.price_version,
       v.description, 0, NOW(), NOW()
FROM (VALUES
  ('content_layout_extract', 'qwen3.8-max',   '通义千问3.8 Max',   '{"max_tokens":3000,"temperature":0.2,"timeout_seconds":120}', 0.012,  0.036,  '2026-10-29-content', '内容获客·从链接提取排版 DNA'),
  ('content_plan',           'qwen3.8-max',   '通义千问3.8 Max',   '{"max_tokens":5000,"temperature":0.5,"timeout_seconds":180}', 0.012,  0.036,  '2026-10-29-content', '内容获客·月度计划说明'),
  ('transcription_cleaning', 'qwen3.8-flash', '通义千问3.8 Flash', '{"max_tokens":4000,"temperature":0.2,"timeout_seconds":120}', 0.0008, 0.0027, '2026-10-29-base',    '转写清洗与说话人识别')
) AS v(function_type, model_code, model_name, model_params, input_token_price, output_token_price, price_version, description)
WHERE NOT EXISTS (
  SELECT 1 FROM llm_model_configs c
  WHERE c.tenant_id = 0 AND c.function_type = v.function_type
    AND c.model_code = v.model_code AND c.deleted_at IS NULL
);
