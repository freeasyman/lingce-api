\echo '== 企业微信运行态备份开始 =='

BEGIN;

CREATE SCHEMA IF NOT EXISTS wecom_reset_backup_20260816;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.wecom_user_bindings;
CREATE TABLE wecom_reset_backup_20260816.wecom_user_bindings AS
SELECT * FROM wecom_user_bindings;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.wecom_corp_installs;
CREATE TABLE wecom_reset_backup_20260816.wecom_corp_installs AS
SELECT * FROM wecom_corp_installs;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.wecom_suite_tickets;
CREATE TABLE wecom_reset_backup_20260816.wecom_suite_tickets AS
SELECT * FROM wecom_suite_tickets;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.wecom_directory_members;
CREATE TABLE wecom_reset_backup_20260816.wecom_directory_members AS
SELECT * FROM wecom_directory_members;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.wecom_event_logs;
CREATE TABLE wecom_reset_backup_20260816.wecom_event_logs AS
SELECT * FROM wecom_event_logs;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.wecom_message_logs;
CREATE TABLE wecom_reset_backup_20260816.wecom_message_logs AS
SELECT * FROM wecom_message_logs;

DROP TABLE IF EXISTS wecom_reset_backup_20260816.tenant_wecom_apps;
CREATE TABLE wecom_reset_backup_20260816.tenant_wecom_apps AS
SELECT * FROM tenant_wecom_apps;

COMMIT;

\echo '== 备份记录数 =='
SELECT 'wecom_user_bindings' AS table_name, COUNT(*) AS row_count FROM wecom_reset_backup_20260816.wecom_user_bindings
UNION ALL
SELECT 'wecom_corp_installs', COUNT(*) FROM wecom_reset_backup_20260816.wecom_corp_installs
UNION ALL
SELECT 'wecom_suite_tickets', COUNT(*) FROM wecom_reset_backup_20260816.wecom_suite_tickets
UNION ALL
SELECT 'wecom_directory_members', COUNT(*) FROM wecom_reset_backup_20260816.wecom_directory_members
UNION ALL
SELECT 'wecom_event_logs', COUNT(*) FROM wecom_reset_backup_20260816.wecom_event_logs
UNION ALL
SELECT 'wecom_message_logs', COUNT(*) FROM wecom_reset_backup_20260816.wecom_message_logs
UNION ALL
SELECT 'tenant_wecom_apps', COUNT(*) FROM wecom_reset_backup_20260816.tenant_wecom_apps
ORDER BY table_name;

\echo '== 企业微信运行态备份完成 =='
