CREATE TABLE IF NOT EXISTS patient_operation_tasks (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    source_id BIGINT REFERENCES patient_conversation_sources(id),
    employee_id BIGINT NOT NULL,
    task_type TEXT NOT NULL,
    goal TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    evidence TEXT NOT NULL DEFAULT '',
    due_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT '待处理',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, source_id, task_type, content)
);
CREATE INDEX IF NOT EXISTS idx_patient_operation_tasks_employee ON patient_operation_tasks(tenant_id, employee_id, status, due_at);
CREATE INDEX IF NOT EXISTS idx_patient_operation_tasks_customer ON patient_operation_tasks(tenant_id, customer_id, created_at DESC);
