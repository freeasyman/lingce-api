-- 留资补基本信息：姓名、职务（机构名/城市已在体检表单里收集）
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS contact_name TEXT NOT NULL DEFAULT '';
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS contact_title TEXT NOT NULL DEFAULT '';
