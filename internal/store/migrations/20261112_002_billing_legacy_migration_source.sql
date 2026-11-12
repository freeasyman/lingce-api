-- 存量租户补发（legacy_migration 批次）需要独立的流水来源，便于按批次对账与撤销。
--
-- 迁移 20261110_002 建的 entitlement_ledger.source CHECK 只允许
-- purchase/gift/trial/rollover/correction/expired；运营要求存量补发的来源标记为
-- legacy_migration + 批次号（批次号写在 operator/reason），这里把取值加进约束。
--
-- 幂等：先 DROP IF EXISTS 再按 pg_constraint 判断重建；计费表不存在时整体跳过。
ALTER TABLE IF EXISTS entitlement_ledger
    DROP CONSTRAINT IF EXISTS entitlement_ledger_source_check;

DO $$
BEGIN
    IF to_regclass('public.entitlement_ledger') IS NOT NULL
       AND NOT EXISTS (
           SELECT 1
             FROM pg_constraint
            WHERE conname = 'entitlement_ledger_source_check'
              AND conrelid = 'public.entitlement_ledger'::regclass
       )
    THEN
        ALTER TABLE public.entitlement_ledger
            ADD CONSTRAINT entitlement_ledger_source_check
            CHECK (source IN ('purchase', 'gift', 'trial', 'rollover', 'correction', 'expired', 'legacy_migration'));
    END IF;
END $$;
