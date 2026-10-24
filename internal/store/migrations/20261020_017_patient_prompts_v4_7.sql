-- v4.7：朋友圈没有问题时不要硬问需求；明确拒绝联系时不要继续销售动作。
UPDATE recording_analysis_prompts
SET
  version = 'v4.7',
  system_prompt = system_prompt || $sys$

## 社交与拒绝联系（v4.7）
- 当截图场景是“朋友圈动态”且没有明确的患者问题时，“帮我回复”只能输出自然的社交回应，或输出空字符串；不得主动询问健康需求、产品兴趣、预算或预约。
- 当患者明确说“不要再联系”“不要发消息”“不考虑”时，“帮我回复”只写一句尊重对方决定的短句，不提问、不推销；“帮我继续跟”只能建议停止联系、记录原因或交主管确认。
$sys$,
  user_prompt_template = user_prompt_template || $user$

补充交付标准：
- 朋友圈无明确问题时，帮我回复不要生成“您是否有健康需求/产品兴趣”的销售问句。
- 患者明确拒绝联系时，帮我回复只做礼貌确认，不追问原因、不推荐产品。
$user$,
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
