-- 计费缴费：补齐 tenant_subscriptions 的服务期列（幂等）。
--
-- 背景：计费模块（开通 / 续费结转 / 每日到期）统一以 started_on / expired_on（DATE）
-- 作为服务期口径（internal/billing/order_store.go、renew_store.go、expire_store.go）。
-- 这两个列在历史生产库里由人工补过，但从未进入版本化迁移：任何按迁移重建的库
-- （演练库、灾备库、本地测试库）都会缺列，首次开通直接报
-- column "started_on" does not exist。
--
-- 生产库已有这两列时本迁移是 no-op（ADD COLUMN IF NOT EXISTS）。
-- 回填只补 NULL 值：旧口径 start_date / end_date 存在时才回填，不覆盖已有服务期。
ALTER TABLE IF EXISTS tenant_subscriptions
    ADD COLUMN IF NOT EXISTS started_on DATE,
    ADD COLUMN IF NOT EXISTS expired_on DATE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'tenant_subscriptions'
          AND column_name = 'start_date'
    ) THEN
        EXECUTE $sql$UPDATE tenant_subscriptions
                       SET started_on = start_date::date
                     WHERE started_on IS NULL
                       AND start_date IS NOT NULL$sql$;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name = 'tenant_subscriptions'
          AND column_name = 'end_date'
    ) THEN
        EXECUTE $sql$UPDATE tenant_subscriptions
                       SET expired_on = end_date::date
                     WHERE expired_on IS NULL
                       AND end_date IS NOT NULL$sql$;
    END IF;
END $$;
