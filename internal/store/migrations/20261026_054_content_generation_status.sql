-- 内容获客：content_items 加 generation_status 持久化生图状态（退出再进入能看出在生成）。
ALTER TABLE content_items ADD COLUMN IF NOT EXISTS generation_status TEXT NOT NULL DEFAULT '';
