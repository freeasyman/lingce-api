-- 机构知识库加「品牌素材」（logo/二维码/医生照片/配色/字体），从旧表迁 logo/二维码。
--
-- 背景：内容创作要照着医院品牌做视觉，但知识库只有文字、缺品牌素材。
-- 旧版 content_org_profiles 里有 logo_url / qr_code_url，这里迁过来。
--
-- 署名：DSH
-- 时间：2026-10-25

ALTER TABLE content_org_knowledge
    ADD COLUMN IF NOT EXISTS brand_assets jsonb;

UPDATE content_org_knowledge k
SET brand_assets = COALESCE(k.brand_assets, '{}'::jsonb) || jsonb_build_object(
    'logo', COALESCE(p.logo_url, ''),
    'qr_code', COALESCE(p.qr_code_url, '')
)
FROM content_org_profiles p
WHERE p.tenant_id = k.tenant_id;
