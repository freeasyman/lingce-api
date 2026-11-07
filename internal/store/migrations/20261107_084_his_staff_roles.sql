-- HIS 员工角色分配（角色可叠加：admin/doctor/pharmacist/cashier/nurse）。
CREATE TABLE IF NOT EXISTS his_staff_roles (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    employee_id BIGINT NOT NULL,
    role TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, employee_id, role),
    CONSTRAINT his_staff_roles_role_check CHECK (role IN ('admin', 'doctor', 'pharmacist', 'cashier', 'nurse'))
);
CREATE INDEX IF NOT EXISTS idx_his_staff_roles_employee ON his_staff_roles(tenant_id, employee_id);
