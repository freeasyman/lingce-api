-- 企业微信回归验证：通讯录同步 / 预绑定结果查询
-- 自建应用验证重点看这个表

SELECT
    id,
    COALESCE(tenant_id, 0) AS tenant_id,
    corp_id,
    COALESCE(wecom_user_id, '') AS wecom_user_id,
    COALESCE(name, '') AS name,
    COALESCE(mobile, '') AS mobile,
    COALESCE(wecom_status, '') AS wecom_status,
    COALESCE(match_status, '') AS match_status,
    matched_employee_id,
    last_synced_at,
    created_at,
    updated_at
FROM wecom_directory_members
WHERE 1 = 1
-- AND tenant_id = 10
-- AND corp_id = 'wwxxxx'
-- AND wecom_user_id = 'zhangsan'
ORDER BY updated_at DESC NULLS LAST, id DESC;
