-- 运营端计费：机构建档补齐档案列（幂等）。
--
-- 背景：运营端「新建机构」只建客户档案，不涉及产品与钱。档案字段里
--   机构全称（开票/通知函用）、联系人职务、省市区、计费模式、客户类型
-- 在 tenants 表里没有位置。本迁移只做加法，全部列可空或带默认值，
-- 对存量租户是 no-op（不写数据、不改已有列）。
--
-- billing_mode 是内部字段：
--   enforce = 额度不足拦截（当前口径）
--   observe = 观察模式（只记录不拦截；本轮只落库，不接扣量逻辑）
-- 租户侧接口（/api/v1/tenants*）不得返回该列；运营端档案也不展示。

ALTER TABLE IF EXISTS tenants
    ADD COLUMN IF NOT EXISTS legal_name TEXT,
    ADD COLUMN IF NOT EXISTS contact_title TEXT,
    ADD COLUMN IF NOT EXISTS province_code TEXT,
    ADD COLUMN IF NOT EXISTS city_code TEXT,
    ADD COLUMN IF NOT EXISTS district_code TEXT,
    ADD COLUMN IF NOT EXISTS billing_mode VARCHAR(16) NOT NULL DEFAULT 'enforce',
    ADD COLUMN IF NOT EXISTS customer_type VARCHAR(16) NOT NULL DEFAULT 'single';

-- 枚举约束单独补：ADD CONSTRAINT 没有 IF NOT EXISTS，按 pg_constraint 判断重建。
DO $$
BEGIN
    IF to_regclass('public.tenants') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1 FROM pg_constraint
           WHERE conname = 'tenants_billing_mode_check'
             AND conrelid = 'public.tenants'::regclass
       )
    THEN
        ALTER TABLE public.tenants
            ADD CONSTRAINT tenants_billing_mode_check
            CHECK (billing_mode IN ('enforce', 'observe'));
    END IF;

    IF to_regclass('public.tenants') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1 FROM pg_constraint
           WHERE conname = 'tenants_customer_type_check'
             AND conrelid = 'public.tenants'::regclass
       )
    THEN
        ALTER TABLE public.tenants
            ADD CONSTRAINT tenants_customer_type_check
            CHECK (customer_type IN ('single', 'group'));
    END IF;
END $$;

-- 存量机构全称回填为空（NULL），运营在档案页补齐即可；industry 已有列直接复用。
-- 代码唯一性沿用 tenants_code_key（区分大小写）；忽略大小写的判重由服务层在写入前校验，
-- 不新增 lower(code) 唯一索引，避免存量数据里的大小写变体阻塞迁移。
