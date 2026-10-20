-- v4.5：客户历史字段统一标注需核对；移除动作提示词中旧的“以截图为准”。
UPDATE recording_analysis_prompts
SET
  version = 'v4.5',
  user_prompt_template = replace(user_prompt_template,
    '【已确认客户上下文】',
    '【客户身份与历史记录（身份已确认；状态/来源/项目/成交/备注为数据库历史，需核对）】'
  ),
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;

UPDATE recording_analysis_prompts
SET
  version = 'v4.5',
  system_prompt = replace(system_prompt,
    'customer_context 只用于确认身份和保持沟通连续，不能覆盖截图事实；冲突时以截图为准并提示人工核对；',
    'customer_context 只用于确认身份和保持沟通连续；截图与客户上下文冲突时，保留双方来源和时间，标记“需核对”，不自动覆盖已确认资料；'
  ),
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
