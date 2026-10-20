-- v4.6：请求主管帮助改为“整理给主管”，只生成材料，不自动发送。
UPDATE recording_analysis_prompts
SET
  version = 'v4.6',
  system_prompt = replace(
    replace(system_prompt,
      '请求主管帮助：整理“已知事实、待确认问题、希望主管决定的一件事”，不重复生成普通销售话术。',
      '整理给主管：整理“已知事实、待确认问题、需要主管决定的一件事”，只生成可转交的材料；不自动发送、不创建主管待办、不生成普通销售话术。'
    ),
    '- 请求主管帮助：',
    '- 整理给主管：'
  ),
  user_prompt_template = replace(
    replace(user_prompt_template,
      '请求主管帮助：整理“已知事实、待确认问题、希望主管决定的一件事”，不重复生成普通销售话术。',
      '整理给主管：整理“已知事实、待确认问题、需要主管决定的一件事”；只生成材料，不自动发送或创建主管待办。'
    ),
    '- 请求主管帮助：',
    '- 整理给主管：'
  ),
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
