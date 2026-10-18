CREATE TABLE IF NOT EXISTS external_data_deliveries (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), tenant_id BIGINT NOT NULL, object_type VARCHAR(32) NOT NULL,
 source_system VARCHAR(128) NOT NULL, source_record_id VARCHAR(255) NOT NULL, idempotency_key VARCHAR(255) NOT NULL,
 payload JSONB NOT NULL, status VARCHAR(16) NOT NULL, resource_id VARCHAR(128), error_reason TEXT,
 attempts INTEGER NOT NULL DEFAULT 0, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(tenant_id, object_type, source_system, source_record_id), UNIQUE(tenant_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS external_data_deliveries_tenant_status_idx ON external_data_deliveries(tenant_id,status,updated_at DESC);
CREATE TABLE IF NOT EXISTS external_data_source_identities (
 id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL, source_system VARCHAR(128) NOT NULL,
 source_patient_id VARCHAR(255) NOT NULL, customer_id BIGINT NOT NULL REFERENCES customers(id),
 match_method VARCHAR(64) NOT NULL, match_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(tenant_id,source_system,source_patient_id)
);
CREATE TABLE IF NOT EXISTS external_data_appointments (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), delivery_id UUID NOT NULL REFERENCES external_data_deliveries(id) ON DELETE CASCADE,
 tenant_id BIGINT NOT NULL, source_system VARCHAR(128) NOT NULL, source_appointment_id VARCHAR(255) NOT NULL,
 customer_id BIGINT, payload JSONB NOT NULL, status VARCHAR(32) NOT NULL, source_updated_at TIMESTAMPTZ,
 UNIQUE(tenant_id,source_system,source_appointment_id)
);
CREATE TABLE IF NOT EXISTS external_data_queues (
 id UUID PRIMARY KEY DEFAULT gen_random_uuid(), delivery_id UUID NOT NULL REFERENCES external_data_deliveries(id) ON DELETE CASCADE,
 tenant_id BIGINT NOT NULL, source_system VARCHAR(128) NOT NULL, source_queue_ticket_id VARCHAR(255) NOT NULL,
 customer_id BIGINT, payload JSONB NOT NULL, status VARCHAR(32) NOT NULL, source_updated_at TIMESTAMPTZ,
 UNIQUE(tenant_id,source_system,source_queue_ticket_id)
);
