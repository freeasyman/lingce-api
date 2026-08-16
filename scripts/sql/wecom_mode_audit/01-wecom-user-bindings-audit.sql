\echo '== 1. wecom_user_bindings 总量 =='
SELECT COUNT(*) AS total_bindings FROM wecom_user_bindings;

\echo '== 2. mode / provider_app 分布 =='
SELECT
  COALESCE(NULLIF(mode, ''), '<empty>') AS mode,
  COALESCE(NULLIF(provider_app, ''), '<empty>') AS provider_app,
  COUNT(*) AS count
FROM wecom_user_bindings
GROUP BY 1, 2
ORDER BY count DESC, mode, provider_app;

\echo '== 3. 空 mode 或空 provider_app =='
SELECT
  id,
  COALESCE(NULLIF(mode, ''), '<empty>') AS mode,
  COALESCE(NULLIF(provider_app, ''), '<empty>') AS provider_app,
  corp_id,
  wecom_user_id,
  employee_id,
  tenant_id,
  source,
  created_at,
  updated_at
FROM wecom_user_bindings
WHERE COALESCE(mode, '') = ''
   OR COALESCE(provider_app, '') = ''
ORDER BY updated_at DESC, id DESC;

\echo '== 4. 按新目标主键检查重复绑定 (mode, provider_app, corp_id, wecom_user_id) =='
SELECT
  mode,
  provider_app,
  corp_id,
  wecom_user_id,
  COUNT(*) AS duplicate_count,
  ARRAY_AGG(id ORDER BY updated_at DESC, id DESC) AS binding_ids
FROM wecom_user_bindings
GROUP BY mode, provider_app, corp_id, wecom_user_id
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, mode, provider_app, corp_id, wecom_user_id;

\echo '== 5. 按旧主键检查重复绑定 (corp_id, wecom_user_id) =='
SELECT
  corp_id,
  wecom_user_id,
  COUNT(*) AS duplicate_count,
  ARRAY_AGG(id ORDER BY updated_at DESC, id DESC) AS binding_ids,
  ARRAY_AGG(COALESCE(NULLIF(mode, ''), '<empty>') ORDER BY updated_at DESC, id DESC) AS modes,
  ARRAY_AGG(COALESCE(NULLIF(provider_app, ''), '<empty>') ORDER BY updated_at DESC, id DESC) AS provider_apps
FROM wecom_user_bindings
GROUP BY corp_id, wecom_user_id
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, corp_id, wecom_user_id;

\echo '== 6. 同一企业微信身份绑定到多个 employee_id =='
SELECT
  corp_id,
  wecom_user_id,
  COUNT(DISTINCT employee_id) AS employee_count,
  ARRAY_AGG(DISTINCT employee_id ORDER BY employee_id) AS employee_ids,
  ARRAY_AGG(id ORDER BY updated_at DESC, id DESC) AS binding_ids
FROM wecom_user_bindings
GROUP BY corp_id, wecom_user_id
HAVING COUNT(DISTINCT employee_id) > 1
ORDER BY employee_count DESC, corp_id, wecom_user_id;

\echo '== 7. 绑定 tenant_id 与员工 tenant_id 不一致 =='
SELECT
  b.id,
  b.mode,
  b.provider_app,
  b.corp_id,
  b.wecom_user_id,
  b.employee_id,
  b.tenant_id AS binding_tenant_id,
  e.tenant_id AS employee_tenant_id,
  e.phone,
  e.full_name,
  b.source,
  b.updated_at
FROM wecom_user_bindings b
JOIN employees e ON e.id = b.employee_id
WHERE b.tenant_id <> e.tenant_id
ORDER BY b.updated_at DESC, b.id DESC;

\echo '== 8. 绑定引用不存在的 employee =='
SELECT
  b.*
FROM wecom_user_bindings b
LEFT JOIN employees e ON e.id = b.employee_id
WHERE e.id IS NULL
ORDER BY b.updated_at DESC, b.id DESC;
