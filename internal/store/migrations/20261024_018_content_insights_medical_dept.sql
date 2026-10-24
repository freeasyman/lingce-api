-- 灵感科室字段改为「临床科室」（内科/外科/中医科），替换掉 017 误加的「部门」（医疗部）。
--
-- 背景：内容产品按「医生账号 / 科室账号」组织，运营要按临床科室（内科/外科/中医科）
-- 筛，而不是组织部门（医疗部/运营部）。017 加的 department_id/department_name 指向
-- departments（部门）表，语义错了，这里用 medical_department_code/name 替换。
--
-- 署名：DSH
-- 时间：2026-10-24

ALTER TABLE content_insights
    ADD COLUMN IF NOT EXISTS medical_department_code TEXT,
    ADD COLUMN IF NOT EXISTS medical_department_name TEXT;

ALTER TABLE content_insights
    DROP COLUMN IF EXISTS department_id,
    DROP COLUMN IF EXISTS department_name;

DROP INDEX IF EXISTS idx_content_insights_department;

CREATE INDEX IF NOT EXISTS idx_content_insights_medical_dept
    ON content_insights (tenant_id, medical_department_code);
