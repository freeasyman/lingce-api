-- 门诊病历评分器大模型提示词和模型配置。
--
-- 新评分链路独立于旧 emr_quality_check，不复用旧质检提示词。

UPDATE public.llm_model_configs
SET is_default = FALSE,
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_score_evaluation'
  AND deleted_at IS NULL;

INSERT INTO public.llm_model_configs (
    tenant_id, model_code, function_type, model_name, provider,
    model_params, extra_params, is_default, is_active, description,
    created_by, created_at, updated_at
)
SELECT
    0,
    'qwen-max',
    'emr_score_evaluation',
    '通义千问 Max',
    'dashscope',
    '{"temperature":0,"max_tokens":6000,"timeout_seconds":120,"response_format":"json"}'::json,
    '{}'::json,
    TRUE,
    TRUE,
    '门诊病历质量评分大模型逐项判断',
    0,
    NOW(),
    NOW()
WHERE NOT EXISTS (
    SELECT 1
    FROM public.llm_model_configs
    WHERE tenant_id = 0
      AND function_type = 'emr_score_evaluation'
      AND model_code = 'qwen-max'
      AND deleted_at IS NULL
);

UPDATE public.llm_model_configs
SET model_name = '通义千问 Max',
    provider = 'dashscope',
    model_params = '{"temperature":0,"max_tokens":6000,"timeout_seconds":120,"response_format":"json"}'::json,
    is_active = TRUE,
    is_default = TRUE,
    description = '门诊病历质量评分大模型逐项判断',
    updated_at = NOW()
WHERE tenant_id = 0
  AND function_type = 'emr_score_evaluation'
  AND model_code = 'qwen-max'
  AND deleted_at IS NULL;

UPDATE public.recording_analysis_prompts
SET is_active = FALSE,
    updated_at = NOW()
WHERE code = 'emr_score_evaluation_v1';

INSERT INTO public.recording_analysis_prompts (
  code, name, description, category,
  system_prompt, user_prompt_template, output_schema,
  version, is_active, usage_count, created_by, updated_by, created_at, updated_at
)
VALUES (
  'emr_score_evaluation_v1',
  '门诊病历质量评分',
  '依据门诊病历质量评定标准逐项判断病历扣分项。',
  'analysis',
  $system$
你是门诊病历质量评分助手。

请严格依据输入中的《门诊病历质量评定标准》，对当前门诊病历的9个评分项目逐项评分。

评分标准是唯一依据，不得自行增加标准之外的扣分项。
只根据当前输入的病历内容判断，不补造事实。

程序已经完成明显空缺项判断，例如字段为空、就诊日期缺失、正式电子签名缺失。
对于程序已经明确发现并扣分的空缺项，不得重复扣分。
除此之外，病历内容是否完整、准确、相关、规范，全部依据评分标准进行语义判断。

要求：
1. 每个评分项目必须返回一条结果。
2. 没有明确问题时，不扣分。
3. 无法根据当前病历判断时，返回“无法判断”，扣分为0。
4. 不要因为某项内容没有出现，就推断该项一定需要填写，除非评分标准或病历场景明确要求。
5. 不直接返回总分和病历等级。
6. 不修改程序已经发现的空缺项。
7. 所有扣分必须能够对应评分标准中的具体扣分条目。
8. 证据必须来自当前病历内容，不能编造。
9. 只返回合法JSON，不返回Markdown或其他说明文字。
$system$,
  $user$
请依据以下评分标准，对当前门诊病历逐项评分。

评分标准版本：
{{score_standard_version}}

完整评分标准：
{{score_standard_text}}

就诊类型：
{{visit_type}}

文书类型：
{{document_type}}

科室：
{{department}}

患者基本信息：
{{patient_snapshot}}

当前病历正文：
{{content}}

程序已经发现的明显空缺项：
{{program_findings}}

请严格按照以下JSON结构返回：

{
  "items": [
    {
      "section": "general",
      "section_name": "一般项目",
      "judgement": "符合",
      "deduct_points": 0,
      "issue": "",
      "evidence": "",
      "suggestion": ""
    }
  ]
}

section只能使用以下值：
general、chief_complaint、history、physical_exam、auxiliary_exam、diagnosis、treatment、signature、writing。

judgement只能是：符合、不符合、无法判断。

每个section必须返回一条结果。
deduct_points只能填写0或评分标准明确规定的扣分值。
最终总分和病历等级由后端计算，不要返回总分和等级。
$user$,
  '{"type":"object","required":["items"]}'::json,
  'v1',
  TRUE,
  0,
  0,
  0,
  NOW(),
  NOW()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM public.recording_analysis_prompts
    WHERE code = 'emr_score_evaluation_v1' AND is_active = TRUE
  ) THEN
    RAISE EXCEPTION '门诊病历评分提示词写入失败';
  END IF;
  IF NOT EXISTS (
    SELECT 1 FROM public.llm_model_configs
    WHERE function_type = 'emr_score_evaluation'
      AND is_active = TRUE
      AND deleted_at IS NULL
  ) THEN
    RAISE EXCEPTION '门诊病历评分模型配置写入失败';
  END IF;
END $$;
