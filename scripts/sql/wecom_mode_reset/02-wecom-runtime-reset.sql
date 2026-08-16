\echo '== 企业微信运行态清理开始 =='

BEGIN;

TRUNCATE TABLE
  wecom_user_bindings,
  wecom_corp_installs,
  wecom_suite_tickets,
  wecom_directory_members
RESTART IDENTITY;

COMMIT;

\echo '== 当前保留表记录数 =='
SELECT 'tenant_wecom_apps' AS table_name, COUNT(*) AS row_count FROM tenant_wecom_apps
UNION ALL
SELECT 'wecom_event_logs', COUNT(*) FROM wecom_event_logs
UNION ALL
SELECT 'wecom_message_logs', COUNT(*) FROM wecom_message_logs
ORDER BY table_name;

\echo '== 企业微信运行态清理完成 =='
