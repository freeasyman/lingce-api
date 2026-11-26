-- 记录 IP 归属城市（离线 ip2region 解析）
ALTER TABLE free_checkup_usage ADD COLUMN IF NOT EXISTS ip_city TEXT NOT NULL DEFAULT '';
ALTER TABLE free_checkup_leads ADD COLUMN IF NOT EXISTS ip_city TEXT NOT NULL DEFAULT '';
