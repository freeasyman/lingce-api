-- 同一机构+问题 24 小时内复用结果：使用记录标记是否命中缓存
ALTER TABLE free_checkup_usage ADD COLUMN IF NOT EXISTS cached BOOLEAN NOT NULL DEFAULT FALSE;
