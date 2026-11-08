-- 赢客专家遗留清理：删除旧 pipeline 审计表、旧 v1.1 提示词行；
-- card-actions 改名 analysis-actions；v0.16 提示词任务字段统一为 sales_tasks。

-- 1. 删除旧 pipeline 审计表（代码已随 pipeline 删除，零引用）
DROP TABLE IF EXISTS deal_expert_analysis_attempt_events CASCADE;
DROP TABLE IF EXISTS deal_expert_analysis_compilations CASCADE;
DROP TABLE IF EXISTS deal_expert_analysis_runs CASCADE;
DROP TABLE IF EXISTS deal_expert_pipeline_bundles CASCADE;

-- 2. 删除旧 v1.1 结案卡提示词行（保留 deal_expert_consultation_analysis v0.16）
DELETE FROM recording_analysis_prompts WHERE code = 'deal_expert_analysis';

-- 3. card-actions 改名 analysis-actions
ALTER TABLE IF EXISTS deal_card_actions RENAME TO analysis_actions;
ALTER INDEX IF EXISTS idx_deal_card_actions_recording RENAME TO idx_analysis_actions_recording;

-- 4. 提示词任务字段统一：follow_up_tasks → sales_tasks（模型输出直接对齐新字段名）
UPDATE recording_analysis_prompts
SET system_prompt = REPLACE(system_prompt, 'follow_up_tasks', 'sales_tasks'),
    user_prompt_template = REPLACE(user_prompt_template, 'follow_up_tasks', 'sales_tasks'),
    updated_at = NOW()
WHERE code = 'deal_expert_consultation_analysis';
