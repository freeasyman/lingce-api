-- 统一随访任务时间字段契约，避免模型继续返回旧格式。
-- 幂等：按提示词 code 和固定标记追加一次。
-- 署名：Codex；时间：2026-10-10。

UPDATE recording_analysis_prompts
SET
  system_prompt = system_prompt || $followup_time_contract$

十四、时间依据字段（必须输出）
每条任务必须额外输出 time_basis，说明联系时间的事实来源，只能填写以下之一：
- doctor_explicit：医生明确说出的日期或相对时间；
- objective_node：明确的复诊、检查或治疗节点；
- hospital_default：确有必要联系但医生未指定时间，由医院默认规则安排；
如果没有明确后续动作或无法确定合理联系时间，输出空数组，不得编造日期。
最终每条任务必须包含：title、contact_time、time_basis、purpose、background、script、phone_script、wechat_content、evidence、contact_method、executor、category、editable_fields。
$followup_time_contract$,
  user_prompt_template = user_prompt_template || $followup_user_contract$

补充要求：JSON 中每条任务必须包含 time_basis 字段，取值只能是 doctor_explicit、objective_node 或 hospital_default；不得省略。
$followup_user_contract$,
  updated_at = NOW()
WHERE code = 'followup_task_generation'
  AND position('十四、时间依据字段（必须输出）' IN system_prompt) = 0;
