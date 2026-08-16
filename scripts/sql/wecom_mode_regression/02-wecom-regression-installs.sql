-- 企业微信回归验证：企业实例结果查询
-- 第三方模式重点看这个表

SELECT
    COALESCE(mode, '') AS mode,
    COALESCE(provider_app, '') AS provider_app,
    COALESCE(tenant_id, 0) AS tenant_id,
    corp_id,
    COALESCE(corp_name, '') AS corp_name,
    COALESCE(agent_id, 0) AS agent_id,
    COALESCE(status, '') AS status,
    CASE WHEN COALESCE(permanent_code, '') <> '' THEN true ELSE false END AS has_permanent_code,
    created_at,
    updated_at,
    cancelled_at
FROM wecom_corp_installs
WHERE 1 = 1
-- AND mode = 'partner_standard'
-- AND provider_app = 'wwxxxx'
-- AND corp_id = 'wwyyyy'
-- AND tenant_id = 10
ORDER BY updated_at DESC NULLS LAST, corp_id ASC;
