-- 品牌素材从「对象」改成「资产列表」。
--
-- 背景：品牌素材要做成可列表展示/新建/编辑/删除，logo 可多个（不同颜色样式）、
-- 二维码可多个、配色一条条。把原来的 {logo, qr_codes, colors} 对象
-- 摊平成 [{type, url?, value?, label}] 数组。
--
-- 署名：DSH
-- 时间：2026-10-25

UPDATE content_org_knowledge k
SET brand_assets = (
    SELECT COALESCE(jsonb_agg(e), '[]'::jsonb)
    FROM (
        SELECT jsonb_build_object('type', 'logo', 'url', k.brand_assets->>'logo', 'label', '') AS e
        WHERE COALESCE(k.brand_assets->>'logo', '') <> ''
        UNION ALL
        SELECT jsonb_build_object('type', 'qr_code', 'url', q->>'url', 'label', COALESCE(q->>'label', ''))
        FROM jsonb_array_elements(COALESCE(k.brand_assets->'qr_codes', '[]'::jsonb)) AS q
        WHERE COALESCE(q->>'url', '') <> ''
        UNION ALL
        SELECT jsonb_build_object('type', 'color', 'value', k.brand_assets->'colors'->>'primary', 'label', '主色')
        WHERE COALESCE(k.brand_assets->'colors'->>'primary', '') <> ''
        UNION ALL
        SELECT jsonb_build_object('type', 'color', 'value', k.brand_assets->'colors'->>'secondary', 'label', '辅色')
        WHERE COALESCE(k.brand_assets->'colors'->>'secondary', '') <> ''
    ) AS s(e)
)
WHERE k.brand_assets IS NOT NULL AND jsonb_typeof(k.brand_assets) = 'object';
