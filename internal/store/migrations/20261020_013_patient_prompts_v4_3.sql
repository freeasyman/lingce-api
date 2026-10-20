-- v4.3：消除 v4 基线与 v4.2 修正之间的相互矛盾。
UPDATE recording_analysis_prompts
SET
  version = 'v4.3',
  system_prompt = replace(
    replace(
      replace(system_prompt,
        'customer_context 只用于确认身份和保持沟通连续，不能覆盖截图事实；冲突时以截图为准并提示人工核对。',
        'customer_context 只用于确认身份和保持沟通连续；截图与客户上下文冲突时，保留双方来源和时间，标记“需核对”，不自动覆盖已确认资料。'
      ),
      '不得把预约、到院、购买、付款、诊断、疗效或服务接受写成已经发生。',
      '不得推断预约、到院、购买、付款、诊断、疗效或服务接受已经发生；如果截图明确出现这些内容，写出是谁陈述/截图显示什么，并标注“未核实”，不能等同于系统已确认。'
    ),
    '朋友圈不是医疗咨询。生活内容只能用于自然互动，不要主动寻找医美、抗衰、疾病或产品机会。',
    '朋友圈默认不是医疗咨询；作者明确写出健康问题或求助时，只作为“[朋友圈作者]”的公开表达线索，不从生活内容推断疾病、需求或购买意向。'
  ),
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;

UPDATE recording_analysis_prompts
SET
  version = 'v4.3',
  system_prompt = replace(
    system_prompt,
    '不得使用“有效控制、改善、治愈、保证、根除、最佳”等承诺疗效或结果的表达。涉及效果只能写“需专业评估/由医生判断”，不能替医生承诺训练或治疗结果。',
    '不得承诺个体疗效或结果；“不能保证改善”不是承诺，但“做完就恢复正常”“一定能改善”属于承诺，必须禁止。涉及效果只能写“需专业评估/由医生判断”。'
  ),
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
