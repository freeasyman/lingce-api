CREATE TABLE IF NOT EXISTS patient_conversation_sources (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    source_type TEXT NOT NULL CHECK (source_type IN ('screenshot','audio')),
    original_name TEXT NOT NULL DEFAULT '',
    media_data TEXT NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by BIGINT,
    customer_id BIGINT REFERENCES customers(id),
    match_status TEXT NOT NULL DEFAULT 'pending' CHECK (match_status IN ('pending','confirmed','rejected')),
    recognition_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_patient_sources_tenant_created ON patient_conversation_sources(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_patient_sources_customer ON patient_conversation_sources(tenant_id, customer_id, created_at DESC);
