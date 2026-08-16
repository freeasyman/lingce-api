-- 企业微信回归验证：绑定结果查询
-- 按需要替换下面条件
-- mode: self_built / partner_standard / partner_template
-- provider_app: 自建通常为空字符串；第三方填 suite_id / template_id

SELECT
    id,
    COALESCE(mode, '') AS mode,
    COALESCE(provider_app, '') AS provider_app,
    corp_id,
    wecom_user_id,
    employee_id,
    tenant_id,
    COALESCE(source, '') AS source,
    created_at,
    updated_at
FROM wecom_user_bindings
WHERE 1 = 1
-- AND mode = 'self_built'
-- AND provider_app = ''
-- AND corp_id = 'wwxxxx'
-- AND employee_id = 123
ORDER BY updated_at DESC NULLS LAST, id DESC;
