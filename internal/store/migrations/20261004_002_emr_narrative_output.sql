-- 病历生成契约：仅体格检查为结构化对象，其他栏位为自然语言文字。
BEGIN;

UPDATE emr_fields
SET input_type='长文本', structure_json='{}'::jsonb, updated_at=NOW()
WHERE code IN ('auxiliary_exam', 'prescription', 'disposition');

-- 清除宽泛的结构化对象指令，避免与病历成文要求冲突。
UPDATE recording_analysis_prompts
SET system_prompt=regexp_replace(system_prompt,
      E'(?m)^.*(结构化栏位|结构化字段|input_type|普通栏位值|普通文本栏位).*$', '', 'g'),
    user_prompt_template=regexp_replace(user_prompt_template,
      E'(?m)^.*(结构化栏位|结构化字段|input_type|普通栏位值|普通文本栏位).*$', '', 'g')
WHERE code='emr_working_draft_generation_v1' AND is_active=TRUE;

UPDATE recording_analysis_prompts
SET system_prompt=system_prompt || E'\n\n【病历正文输出契约】\n本次公共栏位调整中，physical_exam（体格检查）保留结构化对象，按提供的结构定义填写体温、血压、脉率、呼吸、血糖、身高、体重、BMI及查体文本，缺失子项留空，不补造数值。模板中其他明确标记为“结构化内容”且提供 structure_json 的专科栏位，仍按各自结构定义返回对象；不能因为本规则将其改成长文本。\nauxiliary_exam（辅助检查）、prescription（处方）和 disposition（处置）必须返回简洁连贯的自然语言字符串，禁止返回对象、数组或“检查名称：”“药品名称：”“备注：”“用法：”“项目：”“目的：”“状态：”等逐项键值对。其余普通文本栏位也返回自然语言字符串。\n保留医生已经执行、计划安排和仅建议的区别；未明确的药名、剂量、用法、疗程不得补造。',
    user_prompt_template=user_prompt_template || E'\n\n【最终输出要求】\nworking_content 中，physical_exam 以及模板中其他明确标记为结构化内容的专科栏位，按各自 structure_json 返回对象；auxiliary_exam、prescription、disposition 和其他普通文本栏位返回自然语言字符串。不要把结构化专科栏位改成长文本，也不要把普通栏位机械输出为键值对。\n辅助检查写作形式示例：影像资料提示椎间盘突出，压迫神经根。\n处方写作形式示例：建议口服消炎镇痛药，以缓解症状、减轻水肿。\n处置写作形式示例：建议绝对卧床休息至少8周，以减轻神经根受压。\n以上仅说明写作形式，不得把示例事实填入本次病历；正文内容只能来自本次接诊材料。\n没有明确内容的普通栏位写空字符串，缺失或矛盾说明放入 unresolved_items。',
    updated_at=NOW()
WHERE code='emr_working_draft_generation_v1' AND is_active=TRUE;

COMMIT;
