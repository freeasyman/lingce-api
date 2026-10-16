UPDATE recording_analysis_prompts
SET user_prompt_template = user_prompt_template || E'\n必须分别输出以下字段：patient_utterances（患者原话数组）、patient_question（患者问题）、concern（顾虑判断）、relationship_clues（关系人/决策人线索数组）、employee_commitments（员工在录音中明确承诺的事项数组，没有则为空数组）、suggested_actions（建议下一步动作数组）、evidence（支持判断的原话或片段数组）、conversation_summary（沟通概括）。只输出 JSON。',
    updated_at = NOW(), version = 'v2'
WHERE code='patient_recording_analysis_v1'
  AND is_active=true
  AND user_prompt_template NOT LIKE '%employee_commitments%';
