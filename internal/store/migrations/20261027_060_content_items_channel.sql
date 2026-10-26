-- 内容获客：内容发布渠道/栏目（公众号深度文 / 官网各栏目），供 AI 投喂与官网内容分类。
-- 署名：DSH
-- 时间：2026-10-27

ALTER TABLE content_items ADD COLUMN IF NOT EXISTS channel TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN content_items.channel IS '发布渠道/栏目：公众号深度文 / 官网-医生页 / 官网-项目页 / 官网-FAQ / 官网-机构介绍';
