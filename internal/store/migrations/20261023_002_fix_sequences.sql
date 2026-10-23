-- 20261023_002_fix_sequences.sql
-- 迁移显式 id 后，同步 serial 序列（避免新插入主键冲突）。

SELECT setval(pg_get_serial_sequence('content_media_accounts', 'id'), (SELECT COALESCE(MAX(id), 1) FROM content_media_accounts));
SELECT setval(pg_get_serial_sequence('content_insights', 'id'), (SELECT COALESCE(MAX(id), 1) FROM content_insights));
SELECT setval(pg_get_serial_sequence('content_topics_v2', 'id'), (SELECT COALESCE(MAX(id), 1) FROM content_topics_v2));
SELECT setval(pg_get_serial_sequence('content_items_v2', 'id'), (SELECT COALESCE(MAX(id), 1) FROM content_items_v2));
SELECT setval(pg_get_serial_sequence('content_creation_schemes', 'id'), (SELECT COALESCE(MAX(id), 1) FROM content_creation_schemes));
