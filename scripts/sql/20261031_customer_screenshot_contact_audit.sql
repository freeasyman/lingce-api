-- Read-only preflight for screenshots already linked before the contact fix.
-- Returns only tenant-level counts; no customer names/accounts are printed.
BEGIN READ ONLY;
WITH clues AS (
 SELECT s.tenant_id,s.customer_id,
   CASE lower(trim(coalesce(s.recognition_payload->>'platform','')))
     WHEN '微信' THEN 'wechat' WHEN 'weixin' THEN 'wechat'
     WHEN '朋友圈' THEN 'wechat' WHEN '微信朋友圈' THEN 'wechat' WHEN 'moments' THEN 'wechat'
     WHEN '企业微信' THEN 'wecom' WHEN '企微' THEN 'wecom'
     WHEN '小红书' THEN 'xiaohongshu' WHEN 'xhs' THEN 'xiaohongshu'
     WHEN '抖音' THEN 'douyin' WHEN '视频号' THEN 'shipinhao' WHEN '微信视频号' THEN 'shipinhao'
     WHEN '' THEN 'unknown' WHEN '未知' THEN 'unknown' WHEN '无法判断' THEN 'unknown'
     ELSE lower(trim(s.recognition_payload->>'platform')) END AS channel,
   trim(coalesce(s.recognition_payload->>'account','')) AS account,
   trim(coalesce(s.recognition_payload->>'nickname','')) AS nickname
 FROM patient_conversation_sources s
 JOIN customers c ON c.id=s.customer_id AND c.tenant_id=s.tenant_id AND c.deleted_at IS NULL
 WHERE s.source_type='screenshot' AND s.customer_id IS NOT NULL
   AND s.analysis_status='succeeded'
), identities AS (
 SELECT customer_id,
   CASE lower(trim(coalesce(nullif(channel,''),channel_type,'')))
     WHEN '微信' THEN 'wechat' WHEN 'weixin' THEN 'wechat'
     WHEN '朋友圈' THEN 'wechat' WHEN '微信朋友圈' THEN 'wechat' WHEN 'moments' THEN 'wechat'
     WHEN '企业微信' THEN 'wecom' WHEN '企微' THEN 'wecom'
     WHEN '小红书' THEN 'xiaohongshu' WHEN 'xhs' THEN 'xiaohongshu'
     WHEN '抖音' THEN 'douyin' WHEN '视频号' THEN 'shipinhao' WHEN '微信视频号' THEN 'shipinhao'
     WHEN '' THEN 'unknown' WHEN '未知' THEN 'unknown' WHEN '无法判断' THEN 'unknown'
     ELSE lower(trim(coalesce(nullif(channel,''),channel_type,''))) END AS channel,
   trim(coalesce(nullif(channel_id,''),external_id,'')) AS account,
   trim(coalesce(nullif(nickname,''),external_name,'')) AS nickname
 FROM customer_identities
), pending AS (
 SELECT c.*, NOT EXISTS (
   SELECT 1 FROM identities i WHERE i.customer_id=c.customer_id AND i.channel=c.channel
     AND ((c.account<>'' AND i.account=c.account) OR (c.account='' AND i.account='' AND i.nickname=c.nickname))
 ) AS missing_contact
 FROM clues c WHERE account<>'' OR nickname<>''
)
SELECT tenant_id,
 count(*) AS linked_screenshots_with_contact_clues,
 count(*) FILTER (WHERE missing_contact) AS screenshots_missing_saved_contact,
 count(DISTINCT customer_id) FILTER (WHERE missing_contact) AS affected_customers,
 count(*) FILTER (WHERE missing_contact AND account='' AND nickname<>'') AS missing_nickname_only,
 count(*) FILTER (WHERE missing_contact AND channel NOT IN ('wechat','wecom','xiaohongshu','douyin','shipinhao')) AS platform_needs_review
FROM pending GROUP BY tenant_id ORDER BY tenant_id;
ROLLBACK;
