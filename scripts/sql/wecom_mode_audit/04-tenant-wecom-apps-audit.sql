\echo '== 1. tenant_wecom_apps 总量 =='
SELECT COUNT(*) AS total_tenant_apps FROM tenant_wecom_apps;

\echo '== 2. enabled / config_confirmed 分布 =='
SELECT
  enabled,
  config_confirmed,
  COUNT(*) AS count
FROM tenant_wecom_apps
GROUP BY enabled, config_confirmed
ORDER BY count DESC, enabled, config_confirmed;

\echo '== 3. 空 corp_id / agent_id / secret_ciphertext =='
SELECT
  id,
  tenant_id,
  corp_id,
  corp_name,
  agent_id,
  CASE WHEN COALESCE(secret_ciphertext, '') = '' THEN '<empty>' ELSE '<present>' END AS secret_state,
  enabled,
  config_confirmed,
  updated_at
FROM tenant_wecom_apps
WHERE COALESCE(corp_id, '') = ''
   OR agent_id = 0
   OR COALESCE(secret_ciphertext, '') = ''
ORDER BY updated_at DESC, id DESC;

\echo '== 4. 同一 tenant_id 多条自建应用配置 =='
SELECT
  tenant_id,
  COUNT(*) AS app_count,
  ARRAY_AGG(id ORDER BY updated_at DESC, id DESC) AS app_ids,
  ARRAY_AGG(corp_id ORDER BY updated_at DESC, id DESC) AS corp_ids
FROM tenant_wecom_apps
GROUP BY tenant_id
HAVING COUNT(*) > 1
ORDER BY app_count DESC, tenant_id;

\echo '== 5. 同一 corp_id 多条自建应用配置 =='
SELECT
  corp_id,
  COUNT(*) AS app_count,
  ARRAY_AGG(id ORDER BY updated_at DESC, id DESC) AS app_ids,
  ARRAY_AGG(tenant_id ORDER BY updated_at DESC, id DESC) AS tenant_ids
FROM tenant_wecom_apps
GROUP BY corp_id
HAVING COUNT(*) > 1
ORDER BY app_count DESC, corp_id;
