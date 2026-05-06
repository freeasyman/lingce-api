-- Lingce Sales Prospects Module
-- Migration: Create tables for sales prospect management
-- Date: 2026-04-29

-- 1. Sales Prospects table
CREATE TABLE IF NOT EXISTS lingce_sales_prospects (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    institution_name VARCHAR(255) NOT NULL DEFAULT '',
    institution_type VARCHAR(100) NOT NULL DEFAULT '',
    institution_scale VARCHAR(100) NOT NULL DEFAULT '',
    region VARCHAR(100) NOT NULL DEFAULT '',
    contact_name VARCHAR(100) NOT NULL DEFAULT '',
    contact_role VARCHAR(100) NOT NULL DEFAULT '',
    contact_phone VARCHAR(50) NOT NULL DEFAULT '',
    contact_wechat VARCHAR(100) NOT NULL DEFAULT '',
    pain_points JSONB DEFAULT '{}',
    decision_stage VARCHAR(50) NOT NULL DEFAULT 'first_contact',
    deal_probability VARCHAR(20) NOT NULL DEFAULT 'low',
    budget_signal VARCHAR(255) NOT NULL DEFAULT '',
    competitor_mentions JSONB DEFAULT '{}',
    decision_chain JSONB DEFAULT '{}',
    internal_supporters TEXT NOT NULL DEFAULT '',
    internal_blockers TEXT NOT NULL DEFAULT '',
    source VARCHAR(100) NOT NULL DEFAULT '',
    assigned_to BIGINT,
    next_action TEXT NOT NULL DEFAULT '',
    next_follow_up_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    won_at TIMESTAMPTZ,
    lost_reason TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Indexes for lingce_sales_prospects
CREATE INDEX IF NOT EXISTS idx_lsp_tenant_id ON lingce_sales_prospects(tenant_id);
CREATE INDEX IF NOT EXISTS idx_lsp_decision_stage ON lingce_sales_prospects(tenant_id, decision_stage);
CREATE INDEX IF NOT EXISTS idx_lsp_deal_probability ON lingce_sales_prospects(tenant_id, deal_probability);
CREATE INDEX IF NOT EXISTS idx_lsp_status ON lingce_sales_prospects(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_lsp_next_follow_up ON lingce_sales_prospects(tenant_id, next_follow_up_at);
CREATE INDEX IF NOT EXISTS idx_lsp_assigned_to ON lingce_sales_prospects(tenant_id, assigned_to);
CREATE INDEX IF NOT EXISTS idx_lsp_created_at ON lingce_sales_prospects(created_at DESC);

-- 2. Prospect-Recording association table
CREATE TABLE IF NOT EXISTS lingce_sales_prospect_recordings (
    id BIGSERIAL PRIMARY KEY,
    prospect_id BIGINT NOT NULL REFERENCES lingce_sales_prospects(id) ON DELETE CASCADE,
    recording_id BIGINT NOT NULL,
    conversation_type VARCHAR(50) NOT NULL DEFAULT '',
    conversation_purpose VARCHAR(50) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lspr_prospect_id ON lingce_sales_prospect_recordings(prospect_id);
CREATE INDEX IF NOT EXISTS idx_lspr_recording_id ON lingce_sales_prospect_recordings(recording_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_lspr_unique ON lingce_sales_prospect_recordings(prospect_id, recording_id);

-- 3. Stage change history table
CREATE TABLE IF NOT EXISTS lingce_sales_prospect_stage_changes (
    id BIGSERIAL PRIMARY KEY,
    prospect_id BIGINT NOT NULL REFERENCES lingce_sales_prospects(id) ON DELETE CASCADE,
    recording_id BIGINT,
    from_stage VARCHAR(50) NOT NULL DEFAULT '',
    to_stage VARCHAR(50) NOT NULL DEFAULT '',
    change_type VARCHAR(50) NOT NULL DEFAULT 'manual',
    reason TEXT NOT NULL DEFAULT '',
    changed_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_lspsc_prospect_id ON lingce_sales_prospect_stage_changes(prospect_id);
CREATE INDEX IF NOT EXISTS idx_lspsc_created_at ON lingce_sales_prospect_stage_changes(created_at DESC);

-- Verification
SELECT 'lingce_sales_prospects' AS table_name, COUNT(*) AS row_count FROM lingce_sales_prospects
UNION ALL
SELECT 'lingce_sales_prospect_recordings', COUNT(*) FROM lingce_sales_prospect_recordings
UNION ALL
SELECT 'lingce_sales_prospect_stage_changes', COUNT(*) FROM lingce_sales_prospect_stage_changes;

-- Rollback (uncomment to execute):
-- DROP TABLE IF EXISTS lingce_sales_prospect_stage_changes;
-- DROP TABLE IF EXISTS lingce_sales_prospect_recordings;
-- DROP TABLE IF EXISTS lingce_sales_prospects;
