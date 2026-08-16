\echo '== 1. 绑定记录在企业 install 中找不到对应实例 =='
SELECT
  b.id AS binding_id,
  b.mode,
  b.provider_app,
  b.corp_id,
  b.wecom_user_id,
  b.employee_id,
  b.tenant_id,
  b.source,
  b.updated_at
FROM wecom_user_bindings b
LEFT JOIN wecom_corp_installs i
  ON i.mode = b.mode
 AND i.provider_app = b.provider_app
 AND i.corp_id = b.corp_id
 AND COALESCE(i.status, 'active') = 'active'
WHERE COALESCE(b.mode, '') <> ''
  AND i.corp_id IS NULL
ORDER BY b.updated_at DESC, b.id DESC;

\echo '== 2. 第三方 install 的 tenant_id 与绑定 tenant_id 不一致 =='
SELECT
  b.id AS binding_id,
  b.mode,
  b.provider_app,
  b.corp_id,
  b.employee_id,
  b.tenant_id AS binding_tenant_id,
  i.tenant_id AS install_tenant_id,
  b.updated_at
FROM wecom_user_bindings b
JOIN wecom_corp_installs i
  ON i.mode = b.mode
 AND i.provider_app = b.provider_app
 AND i.corp_id = b.corp_id
WHERE COALESCE(b.mode, '') <> ''
  AND b.tenant_id <> i.tenant_id
ORDER BY b.updated_at DESC, b.id DESC;

\echo '== 3. 自建应用绑定的 corp_id 在 tenant_wecom_apps 中找不到配置 =='
SELECT
  b.id AS binding_id,
  b.corp_id,
  b.wecom_user_id,
  b.employee_id,
  b.tenant_id,
  b.updated_at
FROM wecom_user_bindings b
LEFT JOIN tenant_wecom_apps a
  ON a.corp_id = b.corp_id
WHERE COALESCE(b.mode, '') IN ('', 'self_built')
  AND a.id IS NULL
ORDER BY b.updated_at DESC, b.id DESC;

\echo '== 4. 第三方 install 但 provider_app 为空 =='
SELECT
  mode,
  provider_app,
  corp_id,
  corp_name,
  tenant_id,
  agent_id,
  status,
  updated_at
FROM wecom_corp_installs
WHERE COALESCE(mode, '') IN ('partner_standard', 'partner_template')
  AND COALESCE(provider_app, '') = ''
ORDER BY updated_at DESC NULLS LAST, corp_id;
