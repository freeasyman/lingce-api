-- 运营端计费：租户归属（渠道 + 销售）统一表（幂等）。
--
-- 产品文档「4. 租户归属必填（渠道 + 销售）」：
--   归属用于业绩、续费责任与运营数据权限；正式租户与试用租户都要能记。
--   历史上试用客户归属写在 trial_customer_ownerships（一张租户一行、无有效期），
--   正式租户没有归属表。本表统一两者，并保留归属变更历史：
--     effective_to IS NULL = 当前有效；换归属时先把旧行 effective_to 关上，再插新行。
--
-- 约束：
--   · 部分唯一索引保证「一个租户同时只有一条当前有效记录」；
--   · effective_to 不早于 effective_from；
--   · source 记录归属来源，便于对账（manual/trial/legacy_trial/migration）。
--
-- 引用：org_id 逻辑引用 ops_organizations，sales_admin_id 逻辑引用 operations_admins。
-- 与计费模块既有风格一致，这里不建外键（历史 trial 数据里可能存在已删除的组织/管理员，
-- 外键会让迁移在存量脏数据上直接失败）；索引覆盖按组织/销售反查租户。
--
-- 历史数据：trial_customer_ownerships 保留不动（仍作为试用归属的历史来源），
-- 本迁移把其中还没有统一归属记录的租户补进来，source = legacy_trial；
-- 已有当前归属的租户不覆盖（ON CONFLICT DO NOTHING）。

CREATE TABLE IF NOT EXISTS tenant_ownerships (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    org_id BIGINT,
    sales_admin_id BIGINT,
    source TEXT NOT NULL DEFAULT 'manual',
    effective_from DATE NOT NULL DEFAULT CURRENT_DATE,
    effective_to DATE,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenant_ownerships_tenant_check CHECK (tenant_id > 0),
    CONSTRAINT tenant_ownerships_window_check CHECK (effective_to IS NULL OR effective_to >= effective_from)
);

-- 一个租户同时只有一条当前有效归属。
CREATE UNIQUE INDEX IF NOT EXISTS tenant_ownerships_current_uq
    ON tenant_ownerships (tenant_id) WHERE effective_to IS NULL;
CREATE INDEX IF NOT EXISTS tenant_ownerships_org_idx
    ON tenant_ownerships (org_id, effective_to);
CREATE INDEX IF NOT EXISTS tenant_ownerships_sales_idx
    ON tenant_ownerships (sales_admin_id, effective_to);
CREATE INDEX IF NOT EXISTS tenant_ownerships_tenant_history_idx
    ON tenant_ownerships (tenant_id, effective_from DESC, id DESC);

DO $$
BEGIN
    IF to_regclass('public.tenant_ownerships') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1 FROM pg_constraint
           WHERE conname = 'tenant_ownerships_source_check'
             AND conrelid = 'public.tenant_ownerships'::regclass
       )
    THEN
        ALTER TABLE public.tenant_ownerships
            ADD CONSTRAINT tenant_ownerships_source_check
            CHECK (source IN ('manual', 'trial', 'legacy_trial', 'migration'));
    END IF;
END $$;

-- 历史试用归属回填：只补「还没有当前有效归属」的租户，不覆盖运营已录入的归属。
DO $$
BEGIN
    IF to_regclass('public.trial_customer_ownerships') IS NOT NULL
       AND to_regclass('public.tenant_ownerships') IS NOT NULL
    THEN
        INSERT INTO tenant_ownerships (tenant_id, org_id, sales_admin_id, source, effective_from, created_at, updated_at)
        SELECT tc.tenant_id,
               tc.owner_org_id,
               tc.sales_owner_user_id,
               'legacy_trial',
               COALESCE(tc.assigned_at::date, CURRENT_DATE),
               COALESCE(tc.assigned_at, NOW()),
               COALESCE(tc.updated_at, NOW())
        FROM trial_customer_ownerships tc
        WHERE NOT EXISTS (
            SELECT 1
            FROM tenant_ownerships o
            WHERE o.tenant_id = tc.tenant_id
              AND o.effective_to IS NULL
        )
        ON CONFLICT DO NOTHING;
    END IF;
END $$;
