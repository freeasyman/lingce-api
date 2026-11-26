-- 线索转化回写：创建机构成功后记下 tenant_id 与转化时间
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS tenant_id BIGINT NOT NULL DEFAULT 0;
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS converted_at TIMESTAMPTZ;
