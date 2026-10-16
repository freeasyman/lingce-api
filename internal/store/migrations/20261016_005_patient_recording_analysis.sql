CREATE TABLE IF NOT EXISTS patient_operation_recording_runs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    recording_id BIGINT NOT NULL,
    encounter_id BIGINT NOT NULL,
    employee_id BIGINT NOT NULL,
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    customer_name TEXT NOT NULL DEFAULT '',
    business_scope TEXT NOT NULL DEFAULT '',
    role_code TEXT NOT NULL DEFAULT '',
    recorded_at TIMESTAMPTZ NOT NULL,
    cleaned_transcript TEXT NOT NULL,
    analysis_pipeline_version TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','succeeded','failed')),
    request_id TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, recording_id)
);
CREATE INDEX IF NOT EXISTS idx_patient_recording_runs_customer ON patient_operation_recording_runs(tenant_id, customer_id, created_at DESC);
