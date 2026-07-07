BEGIN;

CREATE TABLE IF NOT EXISTS badge_devices_v2 (
    id BIGSERIAL PRIMARY KEY,
    device_no TEXT NOT NULL,
    manufacturer_id BIGINT,
    manufacturer_code TEXT NOT NULL DEFAULT '',
    manufacturer_name TEXT,
    app_id TEXT,
    device_uid TEXT,
    hardware_model TEXT,
    badge_status TEXT NOT NULL,
    current_tenant_id BIGINT,
    current_tenant_name TEXT,
    current_employee_id BIGINT,
    current_employee_name TEXT,
    current_employee_phone TEXT,
    assigned_at TIMESTAMP,
    health_level TEXT NOT NULL DEFAULT 'unknown',
    battery_level INTEGER,
    last_online_at TIMESTAMP,
    last_health_check_at TIMESTAMP,
    health_check_result JSONB NOT NULL DEFAULT '{}'::jsonb,
    import_batch_no TEXT,
    acceptance_batch_no TEXT,
    accepted_at TIMESTAMP,
    acceptance_result TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    CONSTRAINT uq_badge_devices_v2_device_no UNIQUE (device_no),
    CONSTRAINT ck_badge_devices_v2_status CHECK (
        badge_status IN (
            'pending_acceptance',
            'in_stock',
            'assigned',
            'returned',
            'unusable',
            'retired'
        )
    ),
    CONSTRAINT ck_badge_devices_v2_health_level CHECK (
        health_level IN ('unknown', 'healthy', 'warning', 'error')
    )
);

CREATE INDEX IF NOT EXISTS idx_badge_devices_v2_status
    ON badge_devices_v2 (badge_status);
CREATE INDEX IF NOT EXISTS idx_badge_devices_v2_tenant
    ON badge_devices_v2 (current_tenant_id);
CREATE INDEX IF NOT EXISTS idx_badge_devices_v2_employee
    ON badge_devices_v2 (current_employee_id);
CREATE INDEX IF NOT EXISTS idx_badge_devices_v2_created_at
    ON badge_devices_v2 (created_at DESC);

CREATE TABLE IF NOT EXISTS badge_assignment_logs_v2 (
    id BIGSERIAL PRIMARY KEY,
    badge_device_id BIGINT NOT NULL REFERENCES badge_devices_v2(id),
    device_no TEXT NOT NULL,
    tenant_id BIGINT,
    tenant_name TEXT,
    employee_id BIGINT,
    employee_name TEXT,
    action TEXT NOT NULL,
    reason TEXT,
    operator_id BIGINT,
    operator_name TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_badge_assignment_logs_v2_action CHECK (
        action IN ('assign', 'reclaim')
    )
);

CREATE INDEX IF NOT EXISTS idx_badge_assignment_logs_v2_device_id
    ON badge_assignment_logs_v2 (badge_device_id, created_at DESC);

CREATE TABLE IF NOT EXISTS badge_tickets_v2 (
    id BIGSERIAL PRIMARY KEY,
    ticket_no TEXT NOT NULL UNIQUE,
    badge_device_id BIGINT NOT NULL REFERENCES badge_devices_v2(id),
    device_no TEXT NOT NULL,
    ticket_type TEXT NOT NULL,
    ticket_status TEXT NOT NULL DEFAULT 'pending',
    target_badge_status TEXT,
    tenant_id BIGINT,
    employee_id BIGINT,
    submitter_id BIGINT NOT NULL,
    reviewer_id BIGINT,
    executor_id BIGINT,
    notes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_badge_tickets_v2_target_status CHECK (
        target_badge_status IS NULL OR target_badge_status IN (
            'pending_acceptance',
            'in_stock',
            'assigned',
            'returned',
            'unusable',
            'retired'
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_badge_tickets_v2_device_id
    ON badge_tickets_v2 (badge_device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_badge_tickets_v2_status
    ON badge_tickets_v2 (ticket_status, created_at DESC);

CREATE TABLE IF NOT EXISTS badge_device_logs_v2 (
    id BIGSERIAL PRIMARY KEY,
    badge_device_id BIGINT NOT NULL REFERENCES badge_devices_v2(id),
    device_no TEXT NOT NULL,
    operation TEXT NOT NULL,
    from_badge_status TEXT,
    to_badge_status TEXT,
    operator_id BIGINT,
    operator_name TEXT,
    operator_type TEXT,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_badge_device_logs_v2_from_status CHECK (
        from_badge_status IS NULL OR from_badge_status IN (
            'pending_acceptance',
            'in_stock',
            'assigned',
            'returned',
            'unusable',
            'retired'
        )
    ),
    CONSTRAINT ck_badge_device_logs_v2_to_status CHECK (
        to_badge_status IS NULL OR to_badge_status IN (
            'pending_acceptance',
            'in_stock',
            'assigned',
            'returned',
            'unusable',
            'retired'
        )
    )
);

CREATE INDEX IF NOT EXISTS idx_badge_device_logs_v2_device_id
    ON badge_device_logs_v2 (badge_device_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_badge_device_logs_v2_operation
    ON badge_device_logs_v2 (operation, created_at DESC);

COMMIT;
