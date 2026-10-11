-- 随访话术必须使用患者姓名，确保任务可直接执行。
-- 署名：Codex；时间：2026-10-06。
UPDATE recording_analysis_prompts
SET system_prompt = replace(
    system_prompt,
    '- script：可以直接发送或说给患者的话；',
    '- script：可以直接发送或说给患者的话；如果 customer_name 有值，必须自然称呼患者姓名；'
),
updated_at = NOW()
WHERE code = 'followup_task_generation'
  AND system_prompt NOT LIKE '%必须自然称呼患者姓名%';
