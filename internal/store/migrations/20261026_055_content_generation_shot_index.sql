-- 内容获客：content_items 加 generation_shot_index 记录正在生成哪一镜（-1=批量）。
ALTER TABLE content_items ADD COLUMN IF NOT EXISTS generation_shot_index INT NOT NULL DEFAULT -1;
