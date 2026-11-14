-- 035：正式客户的新系统迁移试用（叠加层）
-- 只记录新系统试用窗口，不改 tenants.account_mode、原服务期或正式合同。
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS new_system_trial_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS new_system_trial_ends_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tenants_new_system_trial_end
    ON tenants (new_system_trial_ends_at)
    WHERE new_system_trial_ends_at IS NOT NULL;
