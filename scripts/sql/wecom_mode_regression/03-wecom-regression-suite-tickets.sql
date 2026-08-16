-- 企业微信回归验证：suite ticket 查询
-- 第三方模式重点看这个表

SELECT
    COALESCE(mode, '') AS mode,
    COALESCE(provider_app, '') AS provider_app,
    COALESCE(suite_id, '') AS suite_id,
    LEFT(COALESCE(suite_ticket, ''), 12) AS suite_ticket_prefix,
    created_at
FROM wecom_suite_tickets
WHERE 1 = 1
-- AND mode = 'partner_template'
-- AND provider_app = 'dkaxxxx'
ORDER BY created_at DESC, suite_id ASC;
