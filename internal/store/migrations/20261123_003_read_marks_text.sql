-- 合规核查事项 id 是 UUID；已读表的 resource_id 改为文本，统一支持数值 id 与 UUID。
ALTER TABLE user_resource_reads
    ALTER COLUMN resource_id TYPE TEXT USING resource_id::text;
