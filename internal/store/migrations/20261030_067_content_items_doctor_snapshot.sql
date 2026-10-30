-- 内容表保存医生/科室快照：内容 → 选题 → 洞察 → 员工 这条链一旦某环缺失或员工被改名，
-- 内容库的「医生/科室」列就会变成「—」。创建内容时把当时的人和科室落一份快照，
-- 读取时优先用快照，查不到再回退实时关联。

ALTER TABLE content_items ADD COLUMN IF NOT EXISTS doctor_id BIGINT;
ALTER TABLE content_items ADD COLUMN IF NOT EXISTS doctor_name TEXT;
ALTER TABLE content_items ADD COLUMN IF NOT EXISTS department_name TEXT;

COMMENT ON COLUMN content_items.doctor_id IS '医生（员工）ID 快照';
COMMENT ON COLUMN content_items.doctor_name IS '医生姓名快照（创建内容时写入）';
COMMENT ON COLUMN content_items.department_name IS '科室名快照（创建内容时写入）';
