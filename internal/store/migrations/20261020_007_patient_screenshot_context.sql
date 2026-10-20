-- 截图/动作提示词 v3：注入已确认客户上下文，并为“帮我继续跟”约定结构化输出。
UPDATE recording_analysis_prompts
SET
  version = 'v3',
  system_prompt = system_prompt || $sys$
补充：customer_context 只包含已确认的客户身份和有限历史事实；它不是完整病历，也不能覆盖截图中的事实。两者冲突时以截图证据为准，并提示人工核对。
$sys$,
  user_prompt_template = user_prompt_template || $user$

【已确认客户上下文】
{{customer_context}}
$user$,
  updated_at = NOW()
WHERE code = 'patient_screenshot_analysis_v1' AND is_active = true;

UPDATE recording_analysis_prompts
SET
  version = 'v3',
  system_prompt = system_prompt || $sys$
补充：
- customer_context 只用于确认身份和保持沟通连续，不得用它补充截图没有表达的需求、顾虑、决策人或产品兴趣；与截图证据冲突时以截图为准，并提示人工核对。
- 涉及价格、服务内容、适用条件时，只能引用提供的机构产品资料；资料没有或未确认时，明确说需要确认，不得承诺。
- 例外：动作为“帮我继续跟”时，按用户模板要求输出 JSON 对象，不要输出 Markdown 或额外解释。
$sys$,
  user_prompt_template = user_prompt_template || $user$

【客户上下文】
{{customer_context}}

【本动作输出契约】
- 如果动作是“帮我继续跟”，只输出一个合法 JSON 对象：
  {"content":"给员工看的完整下一步说明","next_action":"一句可执行动作","next_contact_at":"建议联系时间；没有明确依据则为空字符串"}
- 其他动作只输出最终内容，不要输出 JSON。
$user$,
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
