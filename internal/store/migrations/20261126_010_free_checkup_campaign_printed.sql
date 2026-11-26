-- 投放码：是否已印刷（下载过海报/二维码），用于改名二次确认与编辑保护
ALTER TABLE free_checkup_campaigns ADD COLUMN IF NOT EXISTS printed_at TIMESTAMPTZ;
