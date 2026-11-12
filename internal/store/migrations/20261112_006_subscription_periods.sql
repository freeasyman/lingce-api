-- 运营端计费：服务期统一表（幂等）。
--
-- 背景：历史服务期散在 tenant_subscriptions（一张租户一行、与套餐/宽限期耦合）。
-- 运营端计费要把「开通 / 续期 / 到期」统一成一条时间线，并保留历史：
--   · 一个租户同时只有一条 active（部分唯一索引）；
--   · 续期 = 顺延当前 active 的 expired_on（不新增行）；
--   · 试用转正式 / 重新开通 = 把旧 active 关成 terminated，再插一条新 active；
--   · 到期任务 = 把 active 关成 expired。
--
-- tenant_subscriptions 保留不动（历史来源，客户侧登录与存量逻辑仍在读它）；
-- 计费开通/续费时两张表同步写，保持一致。
--
-- 生效口径：status=active 且 expired_on >= CURRENT_DATE 才算「当前在服务期内」，
-- 到期任务负责把过期行关成 expired，读接口不依赖任务的执行时机。

CREATE TABLE IF NOT EXISTS subscription_periods (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    started_on DATE NOT NULL,
    expired_on DATE NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    source VARCHAR(32) NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT subscription_periods_tenant_check CHECK (tenant_id > 0),
    CONSTRAINT subscription_periods_window_check CHECK (expired_on >= started_on),
    CONSTRAINT subscription_periods_status_check CHECK (status IN ('active', 'pending', 'expired', 'terminated'))
);

-- 一个租户同时只有一条 active。
CREATE UNIQUE INDEX IF NOT EXISTS subscription_periods_active_uq
    ON subscription_periods (tenant_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS subscription_periods_tenant_idx
    ON subscription_periods (tenant_id, expired_on DESC, id DESC);
CREATE INDEX IF NOT EXISTS subscription_periods_status_expiry_idx
    ON subscription_periods (status, expired_on);

-- source 需要容纳 legacy_subscription（18 字符）；表已存在时补宽，重复执行是 no-op。
ALTER TABLE IF EXISTS subscription_periods
    ALTER COLUMN source TYPE VARCHAR(32);

-- 历史服务期回填：tenant_subscriptions 的 started_on/expired_on（见 20261112_001），
-- 缺列时退回旧口径 start_date/end_date，再退回 tenants.valid_from/valid_to。
-- 已经是过去的 active 直接落成 expired，避免回填后出现「过期但仍 active」的行；
-- 没有完整起止日的历史行跳过（不写半条服务期）。
--
-- 旧口径列（start_date/end_date）在部分环境并不存在，因此用动态 SQL 拼表达式，
-- 不能直接写死列名（否则 PL/pgSQL 在解析整条 INSERT 时就会报 column does not exist）。
DO $$
DECLARE
    start_candidates TEXT[] := ARRAY[]::TEXT[];
    end_candidates   TEXT[] := ARRAY[]::TEXT[];
    status_candidates TEXT[] := ARRAY[]::TEXT[];
    start_expr TEXT;
    end_expr   TEXT;
    status_expr TEXT;
    insert_sql TEXT;
BEGIN
    IF to_regclass('public.tenant_subscriptions') IS NULL
       OR to_regclass('public.subscription_periods') IS NULL THEN
        RETURN;
    END IF;

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'tenant_subscriptions' AND column_name = 'started_on'
    ) THEN
        start_candidates := array_append(start_candidates, 's.started_on');
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'tenant_subscriptions' AND column_name = 'start_date'
    ) THEN
        start_candidates := array_append(start_candidates, 's.start_date::date');
    END IF;
    start_candidates := array_append(start_candidates, 't.valid_from::date');
    start_candidates := array_append(start_candidates, 't.created_at::date');

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'tenant_subscriptions' AND column_name = 'expired_on'
    ) THEN
        end_candidates := array_append(end_candidates, 's.expired_on');
    END IF;
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'tenant_subscriptions' AND column_name = 'end_date'
    ) THEN
        end_candidates := array_append(end_candidates, 's.end_date::date');
    END IF;
    end_candidates := array_append(end_candidates, 't.valid_to::date');

    start_expr := 'COALESCE(' || array_to_string(start_candidates, ', ') || ')';
    end_expr := 'COALESCE(' || array_to_string(end_candidates, ', ') || ')';

    status_candidates := array_append(status_candidates,
        format('WHEN %s < CURRENT_DATE THEN ''expired''', end_expr));
    status_candidates := array_prepend('WHEN lower(COALESCE(s.status, ''active'')) <> ''active'' THEN ''terminated''',
        status_candidates);
    status_expr := 'CASE ' || array_to_string(status_candidates, ' ') || ' ELSE ''active'' END';

    insert_sql := format($sql$
        INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source, created_at, updated_at)
        SELECT s.tenant_id,
               %s,
               %s,
               %s,
               'legacy_subscription',
               COALESCE(s.created_at, NOW()),
               COALESCE(s.updated_at, NOW())
        FROM tenant_subscriptions s
        JOIN tenants t ON t.id = s.tenant_id
        -- 系统租户（tenant_id=0）只承载平台内置数据，不属于任何机构，也不参与计费；
        -- subscription_periods 约束 tenant_id > 0，因此回填时跳过非正数租户。
        WHERE s.tenant_id > 0
          AND %s IS NOT NULL
          AND %s IS NOT NULL
          AND NOT EXISTS (
              SELECT 1 FROM subscription_periods p WHERE p.tenant_id = s.tenant_id
          )
        ON CONFLICT DO NOTHING
    $sql$, start_expr, end_expr, status_expr, start_expr, end_expr);

    EXECUTE insert_sql;
END $$;
