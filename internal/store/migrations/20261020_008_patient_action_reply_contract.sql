-- 动作生成 v3.1：把“帮我回复”的可复制话术与内部分析彻底分开。
UPDATE recording_analysis_prompts
SET
  version = 'v3.1',
  system_prompt = system_prompt || $sys$

补充：动作为“帮我回复”时，最终输出只能包含可直接发送给对方的回复正文；不得输出分析、依据、建议、内部判断、“建议回复”前缀或任何解释。没有明确未解决问题时，正文只能是简短确认需求的问题。
$sys$,
  user_prompt_template = user_prompt_template || $user$

补充交付标准：
- 帮我回复：只写可直接发送的回复正文，不写分析、依据、建议或前缀；没有明确未解决问题时，只写一句确认需求的问题。
$user$,
  updated_at = NOW()
WHERE code = 'patient_action_generation_v1' AND is_active = true;
