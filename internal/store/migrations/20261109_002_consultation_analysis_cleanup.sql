-- 赢客专家「接待记录分析」唯一入口重构：清理决定卡/v2-card/即时卡/分析队列中间态，统一命名。
--
-- 1. 表改名：deal_cards_v2 → consultation_analyses（card 列 → analysis，补 analyzed_at）
ALTER TABLE IF EXISTS deal_cards_v2 RENAME TO consultation_analyses;
ALTER TABLE IF EXISTS consultation_analyses RENAME COLUMN card TO analysis;
ALTER TABLE IF EXISTS consultation_analyses ADD COLUMN IF NOT EXISTS analyzed_at TIMESTAMPTZ;
UPDATE consultation_analyses SET analyzed_at = updated_at WHERE status = 'analyzed' AND analyzed_at IS NULL;

-- 2. 销售任务表改名：deal_follow_ups → sales_tasks
ALTER TABLE IF EXISTS deal_follow_ups RENAME TO sales_tasks;

-- 3. 删除旧中间态表
DROP TABLE IF EXISTS deal_instant_cards;
DROP TABLE IF EXISTS deal_expert_analyses;

-- 4. 提示词改名：deal_expert_v2_card → deal_expert_consultation_analysis（v0.16 保持）
UPDATE recording_analysis_tenant_configs
SET prompt_code = 'deal_expert_consultation_analysis', updated_at = NOW()
WHERE prompt_code = 'deal_expert_v2_card';
UPDATE recording_analysis_prompts
SET code = 'deal_expert_consultation_analysis',
    description = '录音→接待记录分析：判决/七维质检/教练指导/产品机会/工作记录/销售任务（v0.16）',
    updated_at = NOW()
WHERE code = 'deal_expert_v2_card';

-- 5. 清理租户 4 的随访污染（37 条）；只删这些，别动其它
DELETE FROM recording_tasks
WHERE tenant_id = 4 AND (generation_prompt_version IN ('v0.12','v0.16') OR id IN (694,695,696));

-- 6. 清空租户 4 旧销售任务，全量重跑后按新分析结果重建
DELETE FROM sales_tasks WHERE tenant_id = 4;
