-- Make explicit diagnosis extraction mandatory while retaining the no-inference rule.
UPDATE public.recording_analysis_prompts
SET system_prompt = replace(system_prompt,
  '即使录音未明确诊断，也必须保留对象及空字符串子项，并在 unresolved_items 记录缺失。模板必填不允许模型编造诊断来补齐。不得输出 text、name、diagnosis_code 等别名或顶层 icd10_code；中医诊断不得包含 ICD-10。',
  '医生明确说出疾病名称、病理状态、分级结论或诊断判断时，必须提取到 diagnosis；这不属于模型推断。只有没有可靠诊断时才留空，并在 unresolved_items 记录缺失。没有可靠 ICD-10 编码时只留空 icd10_code，不得清空 diagnosis_name。中医诊断必须单独处理：明确的中医病名或证候提取到 tcm_diagnosis，没有明确中医病名或证候时才留空。不得输出 text、name、diagnosis_code 等别名或顶层 icd10_code；中医诊断不得包含 ICD-10。'),
    user_prompt_template = replace(user_prompt_template,
  'diagnosis 必须为 {"diagnosis_name":"","icd10_code":"","basis":""} 形状；tcm 模板的 tcm_diagnosis 必须为 {"diagnosis_name":"","pattern_name":"","basis":""} 形状。两种诊断分别从医生明确表达提取，不能翻译或推断补全。',
  'diagnosis 必须为 {"diagnosis_name":"","icd10_code":"","basis":""} 形状。医生明确说出疾病名称、病理状态、分级结论或诊断判断时必须提取；没有可靠 ICD-10 编码时只留空 icd10_code，不得清空 diagnosis_name。tcm 模板的 tcm_diagnosis 必须为 {"diagnosis_name":"","pattern_name":"","basis":""} 形状；明确中医病名或证候时必须提取。两种诊断分别从医生明确表达提取，不能翻译或推断补全。'),
    version = 'v2',
    updated_at = NOW()
WHERE code = 'emr_working_draft_generation_v1' AND is_active = TRUE;
