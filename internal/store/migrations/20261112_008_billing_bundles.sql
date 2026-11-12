-- 运营端计费：固定套餐（幂等）。
--
-- 产品文档 4.2.1：开通时既可按快捷套餐（到院成交 / 门诊提效 / 获客增长 / 全院增效）
-- 一键选产品，也可逐项勾选。套餐价与单品价不同（套餐更便宜），
-- 因此需要一张后端价目表供「产品与价格」页与开通接口算价，
-- 不能只在 Go 里写死，否则改价要发版。
--
-- 表结构最小化：只存套餐键、名称、套餐价与包含的产品代码；
-- 单品价仍以 product_catalog.annual_price 为准（接口返回时实时汇总 singlePrice）。

CREATE TABLE IF NOT EXISTS billing_bundles (
    bundle_key VARCHAR(32) PRIMARY KEY,
    name TEXT NOT NULL,
    price NUMERIC(12,2) NOT NULL DEFAULT 0,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT billing_bundles_price_check CHECK (price >= 0)
);

INSERT INTO billing_bundles (bundle_key, name, price, items, is_active) VALUES
    ('deal',   '到院成交', 17800, '["yingke","kehu"]'::jsonb, TRUE),
    ('clinic', '门诊提效', 19800, '["tingxie","suifang"]'::jsonb, TRUE),
    ('growth', '获客增长', 27800, '["huoke","yingke","kehu"]'::jsonb, TRUE),
    ('all',    '全院增效', 49800, '["yingke","kehu","suifang","tingxie","hegui","huoke"]'::jsonb, TRUE)
ON CONFLICT (bundle_key) DO NOTHING;
