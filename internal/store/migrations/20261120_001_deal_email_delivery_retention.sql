-- 邮件投递记录按 180 天保留；补 created_at 索引支撑跨租户时间查询与清理。
CREATE INDEX IF NOT EXISTS idx_deal_email_deliveries_created_at
    ON deal_email_deliveries (created_at);
