ALTER TABLE patient_conversation_sources
    ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS analysis_status TEXT NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS analysis_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS analysis_started_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS analysis_completed_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'patient_sources_analysis_status_check'
    ) THEN
        ALTER TABLE patient_conversation_sources
            ADD CONSTRAINT patient_sources_analysis_status_check
            CHECK (analysis_status IN ('pending','processing','succeeded','failed'));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_patient_sources_content_hash
    ON patient_conversation_sources(tenant_id, content_hash)
    WHERE content_hash <> '';

CREATE UNIQUE INDEX IF NOT EXISTS uq_patient_sources_tenant_content_hash
    ON patient_conversation_sources(tenant_id, content_hash)
    WHERE content_hash <> '';

CREATE INDEX IF NOT EXISTS idx_patient_sources_analysis_status
    ON patient_conversation_sources(tenant_id, analysis_status, created_at DESC);
