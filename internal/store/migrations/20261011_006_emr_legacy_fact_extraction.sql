-- Add the proven legacy outpatient evidence-extraction rules to the structured EMR prompt.
UPDATE public.recording_analysis_prompts
SET
  system_prompt = replace(
    replace(system_prompt,
      '即使录音未明确诊断，也必须保留对象及空字符串子项，并在 unresolved_items 记录缺失。模板必填不允许模型编造诊断来补齐。不得输出 text、name、diagnosis_code 等别名或顶层 icd10_code；中医诊断不得包含 ICD-10。',
      '医生明确说出疾病名称、病理状态、分级结论或诊断判断时，必须提取到 diagnosis；这不属于模型推断。只有没有可靠诊断时才留空，并在 unresolved_items 记录缺失。没有可靠 ICD-10 编码时只留空 icd10_code，不得清空 diagnosis_name。中医诊断必须单独处理：明确的中医病名或证候提取到 tcm_diagnosis，没有明确中医病名或证候时才留空。不得输出 text、name、diagnosis_code 等别名或顶层 icd10_code；中医诊断不得包含 ICD-10。'),
    '中医口语辨证可整理为医生原意，例如胃热、气滞、瘀血等；辨证依据放在相应依据字段，不能自行补成未表达的中医病名。四诊、舌象、脉象和治法只写明确内容。饮食或自行购买建议不能当成正式处方。',
    '【门诊医学事实提取原则】\n对话中出现的医学事实必须提取，包括患者直接陈述的症状、病史和用药，医生追问后患者确认的内容，医生口述的体征、检查发现、诊断判断、治疗决策，以及明确的阴性发现。医生明确说出的疾病名称、诊断名称、病理状态或分级结论必须写入对应诊断栏位。医生使用口语或描述性语言表达临床判断时，也要记录为诊断印象并在 basis 中保留依据。禁止的是编造录音中没有出现的事实，不是禁止对已有事实进行归纳整理。\n同一医学事实分散在多轮对话中时必须跨轮合并。患者猜测、医生提问但未获确认、以及模型根据症状自行推导的诊断不得写入诊断。\n有医学事实时，主诉、现病史和治疗方案不能无理由同时或分别留空；相关字段应尽量提取，未提及的字段才留空并记录 unresolved_items。\n\n中医口语辨证可整理为医生原意，例如胃热、气滞、瘀血等；辨证依据放在相应依据字段，不能自行补成未表达的中医病名。四诊、舌象、脉象和治法只写明确内容。饮食或自行购买建议不能当成正式处方。')
WHERE code = 'emr_working_draft_generation_v1' AND is_active = TRUE;

UPDATE public.recording_analysis_prompts
SET
  user_prompt_template = replace(user_prompt_template,
    'diagnosis 必须为 {"diagnosis_name":"","icd10_code":"","basis":""} 形状；tcm 模板的 tcm_diagnosis 必须为 {"diagnosis_name":"","pattern_name":"","basis":""} 形状。两种诊断分别从医生明确表达提取，不能翻译或推断补全。',
    'diagnosis 必须为 {"diagnosis_name":"","icd10_code":"","basis":""} 形状。医生明确说出疾病名称、病理状态、分级结论或诊断判断时必须提取；没有可靠 ICD-10 编码时只留空 icd10_code，不得清空 diagnosis_name。tcm 模板的 tcm_diagnosis 必须为 {"diagnosis_name":"","pattern_name":"","basis":""} 形状；明确中医病名或证候时必须提取。两种诊断分别从医生明确表达提取，不能翻译或推断补全。'),
  version = 'v3',
  updated_at = NOW()
WHERE code = 'emr_working_draft_generation_v1' AND is_active = TRUE;
