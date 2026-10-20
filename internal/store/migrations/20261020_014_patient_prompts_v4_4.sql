-- v4.4：第一轮建议话术也受疗效/诊断边界约束。
UPDATE recording_analysis_prompts
SET
  version = 'v4.4',
  system_prompt = system_prompt || $sys$

## 第一轮建议话术边界（v4.4）
suggested_icebreaker、suggested_reply、suggested_actions、next_action 同样是可对外内容：
- 不得承诺个体疗效或结果，不得替代医生诊断；
- 不得使用“做完就恢复正常”“一定能改善”“保证有效”等表达；
- 只能描述服务流程、可核实的机构事实和下一步确认动作；
- 涉及医疗效果时写“需专业评估/由医生判断”。
$sys$,
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;
