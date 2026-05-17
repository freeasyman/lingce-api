\echo '=== 1) 总体覆盖率 ==='
WITH x AS (
  SELECT
    COUNT(*) AS total_cnt,
    COUNT(*) FILTER (WHERE COALESCE(file_url,'') <> '') AS has_file_url_cnt,
    COUNT(*) FILTER (WHERE COALESCE(oss_key,'') <> '') AS has_oss_key_cnt,
    COUNT(*) FILTER (WHERE COALESCE(file_url,'') LIKE '%aliyuncs.com/%') AS aliyun_url_cnt,
    COUNT(*) FILTER (WHERE COALESCE(file_url,'') LIKE '%myqcloud.com/%') AS tencent_url_cnt,
    COUNT(*) FILTER (WHERE COALESCE(file_url,'') LIKE '%dudutalk.com/%') AS vendor_dudutalk_cnt
  FROM recordings
)
SELECT
  total_cnt,
  has_file_url_cnt,
  has_oss_key_cnt,
  ROUND((has_oss_key_cnt::numeric / NULLIF(total_cnt,0)) * 100, 2) AS oss_key_coverage_pct,
  aliyun_url_cnt,
  tencent_url_cnt,
  vendor_dudutalk_cnt
FROM x;

\echo '=== 2) 按来源/业务范围分布 ==='
SELECT
  COALESCE(source, '(null)') AS source,
  COALESCE(business_scope, '(null)') AS business_scope,
  COUNT(*) AS total_cnt,
  COUNT(*) FILTER (WHERE COALESCE(oss_key,'') <> '') AS oss_cnt,
  COUNT(*) FILTER (WHERE COALESCE(file_url,'') LIKE '%dudutalk.com/%') AS vendor_cnt
FROM recordings
GROUP BY 1,2
ORDER BY total_cnt DESC, source, business_scope;

\echo '=== 3) 异常项：缺 oss_key 但 file_url 非空（应清零） ==='
SELECT
  id, tenant_id, source, business_scope, order_no, file_url, created_at
FROM recordings
WHERE COALESCE(file_url,'') <> ''
  AND COALESCE(oss_key,'') = ''
ORDER BY id DESC
LIMIT 100;

\echo '=== 4) 抽样：最近 20 条录音 URL/oss_key ==='
SELECT
  id, tenant_id, source, business_scope, order_no,
  LEFT(COALESCE(file_url,''), 120) AS file_url_prefix,
  COALESCE(oss_key,'') AS oss_key,
  created_at
FROM recordings
ORDER BY id DESC
LIMIT 20;

\echo '=== 5) 回调链路核验（可替换 order_no） ==='
-- 手工替换此值快速验证某次回调是否已经落 OSS
-- \set verify_order_no 'cb_oss_verify_20260517_1810'
-- SELECT id, order_no, file_url, oss_key, created_at
-- FROM recordings WHERE order_no = :'verify_order_no';
