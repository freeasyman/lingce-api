-- 只读体检：拆 position/availability 之前必须在目标库先跑一遍。
-- 全部为 SELECT，不改任何数据。建议加 statement_timeout 防止扫大表。
SET statement_timeout = '60s';
SET default_transaction_read_only = on;

\echo '== 1. 规模 =='
SELECT count(*) AS devices,
       count(*) FILTER (WHERE deleted_at IS NOT NULL) AS soft_deleted,
       pg_size_pretty(pg_total_relation_size('badge_devices')) AS table_size
FROM badge_devices;

\echo '== 2. 状态分布（未删除） =='
SELECT status, count(*) FROM badge_devices WHERE deleted_at IS NULL GROUP BY 1 ORDER BY 2 DESC;

\echo '== 3. 归属与状态不一致 =='
SELECT count(*) FILTER (WHERE status = 'assigned' AND employee_id IS NULL)        AS assigned_without_employee,
       count(*) FILTER (WHERE status = 'assigned' AND tenant_id IS NULL)          AS assigned_without_tenant,
       count(*) FILTER (WHERE status = 'in_stock' AND employee_id IS NOT NULL)    AS in_stock_with_employee,
       count(*) FILTER (WHERE status = 'retired'  AND employee_id IS NOT NULL)    AS retired_with_employee,
       count(*) FILTER (WHERE accepted_at IS NULL AND status IN ('in_stock','assigned')) AS active_but_never_accepted
FROM badge_devices WHERE deleted_at IS NULL;

\echo '== 4. 重复设备号 =='
SELECT device_no, count(*) FROM badge_devices WHERE deleted_at IS NULL GROUP BY 1 HAVING count(*) > 1 LIMIT 20;

\echo '== 5. 健康快照覆盖（决定 availability 能推导多少） =='
SELECT count(*) FILTER (WHERE health_check_result IS NULL OR health_check_result = '{}'::jsonb) AS no_snapshot,
       count(*) FILTER (WHERE health_status = 'healthy') AS healthy,
       count(*) FILTER (WHERE health_status = 'warning') AS warning,
       count(*) FILTER (WHERE health_status = 'error')   AS error,
       count(*) FILTER (WHERE health_status IS NULL OR health_status = 'unknown') AS unknown
FROM badge_devices WHERE deleted_at IS NULL;

\echo '== 6. 两张日志表的覆盖（量化“历史缺一半”） =='
WITH d AS (SELECT id FROM badge_devices WHERE deleted_at IS NULL)
SELECT (SELECT count(*) FROM d) AS devices,
       (SELECT count(DISTINCT l.device_id) FROM badge_device_lifecycle_logs l JOIN d ON d.id = l.device_id) AS with_lifecycle_log,
       (SELECT count(DISTINCT l.device_id) FROM badge_device_logs l JOIN d ON d.id = l.device_id)           AS with_legacy_log,
       (SELECT count(DISTINCT l.device_id) FROM badge_device_logs l JOIN d ON d.id = l.device_id
         WHERE NOT EXISTS (SELECT 1 FROM badge_device_lifecycle_logs x WHERE x.device_id = l.device_id))    AS legacy_only;

\echo '== 7. 孤儿日志 =='
SELECT (SELECT count(*) FROM badge_device_lifecycle_logs l WHERE NOT EXISTS (SELECT 1 FROM badge_devices b WHERE b.id = l.device_id)) AS lifecycle_orphan,
       (SELECT count(*) FROM badge_device_logs l           WHERE NOT EXISTS (SELECT 1 FROM badge_devices b WHERE b.id = l.device_id)) AS legacy_orphan;

\echo '== 8. 在用但长期掉线 =='
SELECT count(*) FILTER (WHERE last_online_at IS NULL) AS never_online,
       count(*) FILTER (WHERE last_online_at < now() - interval '30 days') AS offline_30d
FROM badge_devices WHERE deleted_at IS NULL AND status = 'assigned';
