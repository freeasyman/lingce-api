CREATE TABLE IF NOT EXISTS patient_operation_records (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    source_id BIGINT REFERENCES patient_conversation_sources(id),
    employee_id BIGINT NOT NULL,
    action_type TEXT NOT NULL,
    record_type TEXT NOT NULL,
    content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'confirmed',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_patient_operation_records_customer ON patient_operation_records(tenant_id, customer_id, created_at DESC);
CREATE TABLE IF NOT EXISTS patient_operation_task_drafts (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    source_id BIGINT REFERENCES patient_conversation_sources(id),
    employee_id BIGINT NOT NULL,
    task_type TEXT NOT NULL,
    goal TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending_confirmation',
    due_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_patient_operation_drafts_customer ON patient_operation_task_drafts(tenant_id, customer_id, created_at DESC);
