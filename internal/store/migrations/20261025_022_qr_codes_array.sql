-- 二维码支持多个：brand_assets.qr_code（单字符串）→ qr_codes（数组）。
--
-- 背景：医院可能有多个二维码（咨询、关注、预约），品牌素材要允许不止一个。
--
-- 署名：DSH
-- 时间：2026-10-25

UPDATE content_org_knowledge
SET brand_assets = (brand_assets - 'qr_code') || jsonb_build_object(
    'qr_codes',
    CASE
      WHEN COALESCE(brand_assets->>'qr_code', '') <> ''
      THEN jsonb_build_array(jsonb_build_object('url', brand_assets->>'qr_code', 'label', ''))
      ELSE '[]'::jsonb
    END
)
WHERE brand_assets ? 'qr_code';
