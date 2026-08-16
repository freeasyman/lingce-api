\echo '== 企业微信运行态清理后检查 =='

SELECT 'wecom_user_bindings' AS table_name, COUNT(*) AS row_count FROM wecom_user_bindings
UNION ALL
SELECT 'wecom_corp_installs', COUNT(*) FROM wecom_corp_installs
UNION ALL
SELECT 'wecom_suite_tickets', COUNT(*) FROM wecom_suite_tickets
UNION ALL
SELECT 'wecom_directory_members', COUNT(*) FROM wecom_directory_members
UNION ALL
SELECT 'tenant_wecom_apps', COUNT(*) FROM tenant_wecom_apps
UNION ALL
SELECT 'wecom_event_logs', COUNT(*) FROM wecom_event_logs
UNION ALL
SELECT 'wecom_message_logs', COUNT(*) FROM wecom_message_logs
ORDER BY table_name;

\echo '== 自建应用保留配置检查 =='
SELECT
  tenant_id,
  corp_id,
  corp_name,
  agent_id,
  enabled,
  config_confirmed
FROM tenant_wecom_apps
ORDER BY tenant_id, id;

\echo '== 第三方运行态应为空 =='
SELECT
  COUNT(*) AS active_partner_installs
FROM wecom_corp_installs
WHERE COALESCE(status, 'active') = 'active';

\echo '== 绑定表应为空 =='
SELECT COUNT(*) AS active_bindings FROM wecom_user_bindings;

\echo '== 清理后检查完成 =='
