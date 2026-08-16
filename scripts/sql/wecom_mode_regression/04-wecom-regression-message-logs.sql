-- 企业微信回归验证：消息投递结果查询
-- 自建与第三方都可用

SELECT
    id,
    COALESCE(mode, '') AS mode,
    COALESCE(provider_app, '') AS provider_app,
    COALESCE(corp_id, '') AS corp_id,
    COALESCE(tenant_id, 0) AS tenant_id,
    COALESCE(employee_id, 0) AS employee_id,
    COALESCE(wecom_user_id, '') AS wecom_user_id,
    COALESCE(message_scene, '') AS message_scene,
    COALESCE(status, '') AS status,
    COALESCE(error_message, '') AS error_message,
    COALESCE(dedupe_key, '') AS dedupe_key,
    created_at,
    updated_at,
    sent_at
FROM wecom_message_logs
WHERE 1 = 1
-- AND mode = 'self_built'
-- AND corp_id = 'wwxxxx'
-- AND employee_id = 123
-- AND message_scene = 'opportunity_alert'
ORDER BY created_at DESC, id DESC
LIMIT 200;
