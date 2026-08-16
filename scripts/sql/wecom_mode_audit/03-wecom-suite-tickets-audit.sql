\echo '== 1. wecom_suite_tickets 总量 =='
SELECT COUNT(*) AS total_tickets FROM wecom_suite_tickets;

\echo '== 2. mode / provider_app / suite_id 分布 =='
SELECT
  COALESCE(NULLIF(mode, ''), '<empty>') AS mode,
  COALESCE(NULLIF(provider_app, ''), '<empty>') AS provider_app,
  suite_id,
  COUNT(*) AS count
FROM wecom_suite_tickets
GROUP BY 1, 2, 3
ORDER BY count DESC, mode, provider_app, suite_id;

\echo '== 3. 空 mode 或空 provider_app =='
SELECT
  id,
  COALESCE(NULLIF(mode, ''), '<empty>') AS mode,
  COALESCE(NULLIF(provider_app, ''), '<empty>') AS provider_app,
  suite_id,
  created_at
FROM wecom_suite_tickets
WHERE COALESCE(mode, '') = ''
   OR COALESCE(provider_app, '') = ''
ORDER BY created_at DESC, id DESC;

\echo '== 4. 同一 mode/provider_app 下是否出现多个 suite_id =='
SELECT
  mode,
  provider_app,
  COUNT(DISTINCT suite_id) AS suite_id_count,
  ARRAY_AGG(DISTINCT suite_id ORDER BY suite_id) AS suite_ids
FROM wecom_suite_tickets
GROUP BY mode, provider_app
HAVING COUNT(DISTINCT suite_id) > 1
ORDER BY suite_id_count DESC, mode, provider_app;
