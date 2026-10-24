-- 灵感固化来源医生/科室：提取时写入，避免 JOIN 带来的历史归属漂移。
--
-- 背景：内容产品按「医生账号 / 科室账号」组织，灵感需要稳定知道来自哪位医生、
-- 哪个科室。若每次靠 JOIN（录音→医生→科室），医生换科室/离职后历史归属会跟着变。
-- 因此提取那一刻把医生/科室固化成冗余列。
--
-- 署名：DSH
-- 时间：2026-10-24

ALTER TABLE content_insights
    ADD COLUMN IF NOT EXISTS employee_id INTEGER,
    ADD COLUMN IF NOT EXISTS department_id INTEGER,
    ADD COLUMN IF NOT EXISTS employee_name TEXT,
    ADD COLUMN IF NOT EXISTS department_name TEXT;

CREATE INDEX IF NOT EXISTS idx_content_insights_employee
    ON content_insights (tenant_id, employee_id);

CREATE INDEX IF NOT EXISTS idx_content_insights_department
    ON content_insights (tenant_id, department_id);
