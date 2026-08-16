\echo '== 1. wecom_corp_installs 总量 =='
SELECT COUNT(*) AS total_installs FROM wecom_corp_installs;

\echo '== 2. mode / provider_app / status 分布 =='
SELECT
  COALESCE(NULLIF(mode, ''), '<empty>') AS mode,
  COALESCE(NULLIF(provider_app, ''), '<empty>') AS provider_app,
  COALESCE(NULLIF(status, ''), '<empty>') AS status,
  COUNT(*) AS count
FROM wecom_corp_installs
GROUP BY 1, 2, 3
ORDER BY count DESC, mode, provider_app, status;

\echo '== 3. 空 mode / provider_app / tenant_id / corp_id / agent_id / permanent_code =='
SELECT
  COALESCE(NULLIF(mode, ''), '<empty>') AS mode,
  COALESCE(NULLIF(provider_app, ''), '<empty>') AS provider_app,
  tenant_id,
  corp_id,
  corp_name,
  agent_id,
  CASE WHEN COALESCE(permanent_code, '') = '' THEN '<empty>' ELSE '<present>' END AS permanent_code_state,
  status,
  updated_at
FROM wecom_corp_installs
WHERE COALESCE(mode, '') = ''
   OR COALESCE(provider_app, '') = ''
   OR COALESCE(corp_id, '') = ''
   OR tenant_id = 0
   OR agent_id = 0
   OR COALESCE(permanent_code, '') = ''
ORDER BY updated_at DESC, corp_id;

\echo '== 4. 按新目标主键检查重复 install (mode, provider_app, corp_id) =='
SELECT
  mode,
  provider_app,
  corp_id,
  COUNT(*) AS duplicate_count,
  ARRAY_AGG(corp_id ORDER BY updated_at DESC NULLS LAST, corp_id) AS install_keys
FROM wecom_corp_installs
GROUP BY mode, provider_app, corp_id
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, mode, provider_app, corp_id;

\echo '== 5. 按旧主键检查重复 install (corp_id) =='
SELECT
  corp_id,
  COUNT(*) AS duplicate_count,
  ARRAY_AGG(corp_id ORDER BY updated_at DESC NULLS LAST, corp_id) AS install_keys,
  ARRAY_AGG(COALESCE(NULLIF(mode, ''), '<empty>') ORDER BY updated_at DESC NULLS LAST, corp_id) AS modes,
  ARRAY_AGG(COALESCE(NULLIF(provider_app, ''), '<empty>') ORDER BY updated_at DESC NULLS LAST, corp_id) AS provider_apps
FROM wecom_corp_installs
GROUP BY corp_id
HAVING COUNT(*) > 1
ORDER BY duplicate_count DESC, corp_id;

\echo '== 6. install 引用不存在的 tenant =='
SELECT
  i.*
FROM wecom_corp_installs i
LEFT JOIN tenants t ON t.id = i.tenant_id
WHERE i.tenant_id <> 0
  AND t.id IS NULL
ORDER BY i.updated_at DESC NULLS LAST, i.corp_id;
