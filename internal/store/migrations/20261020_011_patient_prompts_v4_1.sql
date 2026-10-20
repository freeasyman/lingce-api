-- v4.1：修正朋友圈作者原话边界；动作输出禁止疗效承诺。
UPDATE recording_analysis_prompts
SET
  version = 'v4.1',
  system_prompt = system_prompt || $sys$

## 朋友圈边界（v4.1 补充）
当 content_scene=朋友圈动态 时：
- 朋友圈作者的生活文案不是患者/家属原话，patient_utterances 必须为空数组；
- 作者文字只进入 evidence，并以“[朋友圈作者]”开头；
- 不在朋友圈生活内容里推断患者问题、顾虑、决策人、产品兴趣或医疗需求；
- 除非截图明确同时包含患者/家属与机构的聊天，否则 patient_question、concern、decision_maker 也保持为空。
$sys$,
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;

UPDATE recording_analysis_prompts
SET
  version = 'v4.1',
  system_prompt = system_prompt || $sys$

## 疗效表达边界（v4.1 补充）
不得使用“有效控制、改善、治愈、保证、根除、最佳”等承诺疗效或结果的表达。涉及效果只能写“需专业评估/由医生判断”，不能替医生承诺训练或治疗结果。
$sys$,
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
