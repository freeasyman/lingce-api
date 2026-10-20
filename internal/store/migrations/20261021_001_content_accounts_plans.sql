-- Content-center accounts, columns and weekly plan slots.
-- These are content-domain tables; they do not touch Worker/follow-up/EMR/deal tables.

CREATE TABLE IF NOT EXISTS content_accounts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(120) NOT NULL,
    platform VARCHAR(30) NOT NULL,
    account_type VARCHAR(30) NOT NULL DEFAULT 'institution',
    status VARCHAR(20) NOT NULL DEFAULT 'normal',
    owner_name VARCHAR(80),
    reviewer_name VARCHAR(80),
    audience VARCHAR(500),
    forms JSONB NOT NULL DEFAULT '[]'::jsonb,
    notes TEXT,
    extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_content_accounts_tenant
  ON content_accounts (tenant_id, updated_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS content_account_columns (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL REFERENCES content_accounts(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    form VARCHAR(50) NOT NULL,
    goal VARCHAR(500),
    cadence VARCHAR(50) NOT NULL DEFAULT 'weekly',
    weekly_target INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_content_account_columns_account
  ON content_account_columns (tenant_id, account_id, sort_order) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS content_plan_slots (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL REFERENCES content_accounts(id) ON DELETE CASCADE,
    column_id BIGINT NOT NULL REFERENCES content_account_columns(id) ON DELETE CASCADE,
    week_start DATE NOT NULL,
    target_count INTEGER NOT NULL DEFAULT 1,
    status VARCHAR(20) NOT NULL DEFAULT 'planned',
    notes TEXT,
    extra_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, account_id, column_id, week_start)
);
CREATE INDEX IF NOT EXISTS idx_content_plan_slots_week
  ON content_plan_slots (tenant_id, week_start, account_id);
