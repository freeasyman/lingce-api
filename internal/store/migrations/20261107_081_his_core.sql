-- HIS 核心域表（P1）。设计基线：docs/his/02-域模型-v0.1。
-- 原则：复用 customers/encounters/emr_records/departments/employees，不建第二套患者主档。

-- 2.1 主数据域

CREATE TABLE IF NOT EXISTS his_doctors (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    employee_id BIGINT NOT NULL,
    department_id BIGINT,
    title TEXT NOT NULL DEFAULT '',
    license_no TEXT NOT NULL DEFAULT '',
    is_prescribing BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, employee_id)
);
CREATE INDEX IF NOT EXISTS idx_his_doctors_department ON his_doctors(tenant_id, department_id, status);

CREATE TABLE IF NOT EXISTS his_rooms (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    type TEXT NOT NULL DEFAULT 'diagnose', -- diagnose/treatment/pharmacy
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS his_drugs (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    spec TEXT NOT NULL DEFAULT '',
    unit TEXT NOT NULL DEFAULT '',
    manufacturer TEXT NOT NULL DEFAULT '',
    drug_type TEXT NOT NULL DEFAULT 'western', -- western/chinese_patent/chinese_herb/supplies
    retail_price NUMERIC(12,2) NOT NULL DEFAULT 0,
    purchase_price NUMERIC(12,2) NOT NULL DEFAULT 0,
    is_rx BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);
CREATE INDEX IF NOT EXISTS idx_his_drugs_name ON his_drugs(tenant_id, name, status);

CREATE TABLE IF NOT EXISTS his_charge_items (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'other', -- 诊查/治疗/检查/检验/其他
    unit TEXT NOT NULL DEFAULT '次',
    price NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS his_bundles (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    price NUMERIC(12,2) NOT NULL DEFAULT 0, -- 打包价，独立于原子价之和
    description TEXT NOT NULL DEFAULT '',
    valid_from DATE,
    valid_to DATE,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE TABLE IF NOT EXISTS his_bundle_items (
    id BIGSERIAL PRIMARY KEY,
    bundle_id BIGINT NOT NULL REFERENCES his_bundles(id) ON DELETE CASCADE,
    charge_item_id BIGINT NOT NULL REFERENCES his_charge_items(id),
    quantity NUMERIC(12,2) NOT NULL DEFAULT 1,
    UNIQUE (bundle_id, charge_item_id)
);

-- 病案号注册表：一人一号、首诊发放、永不复用；预留外部 HIS 病案号挂接。
CREATE TABLE IF NOT EXISTS patient_identifiers (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL,
    identifier_type TEXT NOT NULL, -- mrn / external_his_no
    value TEXT NOT NULL,
    issued_by TEXT NOT NULL DEFAULT '',
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, identifier_type, value)
);
CREATE INDEX IF NOT EXISTS idx_patient_identifiers_customer ON patient_identifiers(tenant_id, customer_id);

-- 租户内编号流水：mrn=病案号(M+8位) rx=处方号(P+日期+4位) order=收费单号(O+日期+4位)
CREATE TABLE IF NOT EXISTS his_number_counters (
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    scope TEXT NOT NULL,
    last_seq BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, scope)
);

-- 2.2 排班与号源域

CREATE TABLE IF NOT EXISTS his_schedules (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    doctor_id BIGINT NOT NULL REFERENCES his_doctors(id),
    department_id BIGINT,
    room_id BIGINT REFERENCES his_rooms(id),
    work_date DATE NOT NULL,
    period TEXT NOT NULL DEFAULT 'morning', -- morning/afternoon/evening
    start_time TIME NOT NULL,
    end_time TIME NOT NULL,
    slot_interval_minutes INT NOT NULL DEFAULT 15,
    status TEXT NOT NULL DEFAULT 'open', -- open/suspended
    remark TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_schedules_query ON his_schedules(tenant_id, work_date, department_id, doctor_id, status);

CREATE TABLE IF NOT EXISTS his_slots (
    id BIGSERIAL PRIMARY KEY,
    schedule_id BIGINT NOT NULL REFERENCES his_schedules(id) ON DELETE CASCADE,
    seq_no INT NOT NULL,
    time_start TIME NOT NULL,
    time_end TIME NOT NULL,
    status TEXT NOT NULL DEFAULT 'free', -- free/reserved/registered/disabled
    registration_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (schedule_id, seq_no)
);
CREATE INDEX IF NOT EXISTS idx_his_slots_registration ON his_slots(registration_id);

-- 2.3 挂号与就诊域

CREATE TABLE IF NOT EXISTS his_registrations (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL,
    department_id BIGINT,
    doctor_id BIGINT,
    schedule_id BIGINT,
    slot_id BIGINT,
    reg_type TEXT NOT NULL DEFAULT 'walkin', -- walkin/appointment
    source TEXT NOT NULL DEFAULT 'desk', -- desk/miniprogram/h5
    visit_date DATE NOT NULL,
    fee NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'registered', -- reserved/registered/visited/refunded/no_show
    queue_no TEXT NOT NULL DEFAULT '',
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- 同一号源同时只能有一个生效挂号/预约；退号/爽约后的历史行不参与唯一约束。
CREATE UNIQUE INDEX IF NOT EXISTS uq_his_registrations_active_slot
    ON his_registrations (schedule_id, slot_id)
    WHERE status IN ('reserved', 'registered');
CREATE INDEX IF NOT EXISTS idx_his_registrations_query ON his_registrations(tenant_id, visit_date, status, department_id);
CREATE INDEX IF NOT EXISTS idx_his_registrations_customer ON his_registrations(tenant_id, customer_id, visit_date DESC);

CREATE TABLE IF NOT EXISTS his_queue (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    encounter_id BIGINT NOT NULL,
    registration_id BIGINT,
    seq_no INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'waiting', -- waiting/in_progress/done/skipped
    called_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (encounter_id)
);
CREATE INDEX IF NOT EXISTS idx_his_queue_desk ON his_queue(tenant_id, status, seq_no);

-- 2.4 临床域

CREATE TABLE IF NOT EXISTS his_prescriptions (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    encounter_id BIGINT NOT NULL,
    customer_id BIGINT NOT NULL,
    doctor_id BIGINT,
    prescription_no TEXT,
    type TEXT NOT NULL DEFAULT 'western', -- western/herbal/mixed
    diagnosis TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft', -- draft/issued/verified/charged/dispensed/void
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, prescription_no)
);
CREATE INDEX IF NOT EXISTS idx_his_prescriptions_query ON his_prescriptions(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_his_prescriptions_encounter ON his_prescriptions(tenant_id, encounter_id);

CREATE TABLE IF NOT EXISTS his_prescription_items (
    id BIGSERIAL PRIMARY KEY,
    prescription_id BIGINT NOT NULL REFERENCES his_prescriptions(id) ON DELETE CASCADE,
    drug_id BIGINT,
    item_type TEXT NOT NULL DEFAULT 'drug', -- drug/herb
    name TEXT NOT NULL,
    spec TEXT NOT NULL DEFAULT '',
    dose TEXT NOT NULL DEFAULT '',
    dose_unit TEXT NOT NULL DEFAULT '',
    frequency TEXT NOT NULL DEFAULT '',
    route TEXT NOT NULL DEFAULT '',
    days INT,
    quantity NUMERIC(12,2) NOT NULL DEFAULT 0,
    unit TEXT NOT NULL DEFAULT '',
    price NUMERIC(12,2) NOT NULL DEFAULT 0,
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    usage_text TEXT NOT NULL DEFAULT '' -- 中药饮片煎服法/先煎后下等
);
CREATE INDEX IF NOT EXISTS idx_his_prescription_items_rx ON his_prescription_items(prescription_id);

CREATE TABLE IF NOT EXISTS his_service_requests (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    encounter_id BIGINT NOT NULL,
    customer_id BIGINT NOT NULL,
    request_type TEXT NOT NULL DEFAULT 'exam', -- exam/lab
    item_name TEXT NOT NULL,
    specimen_type TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'ordered', -- ordered/charged/executed/reported
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_service_requests_encounter ON his_service_requests(tenant_id, encounter_id);

-- 治疗医嘱（P1 简化版：开单 → 收费 → 执行登记，不做配对门禁与疗程锁定）
CREATE TABLE IF NOT EXISTS his_treatment_orders (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    encounter_id BIGINT NOT NULL,
    customer_id BIGINT NOT NULL,
    charge_item_id BIGINT,
    plan_total_times INT NOT NULL DEFAULT 1,
    completed_times INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'ordered', -- ordered/charged/executing/completed
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_treatment_orders_encounter ON his_treatment_orders(tenant_id, encounter_id);

CREATE TABLE IF NOT EXISTS his_treatment_executions (
    id BIGSERIAL PRIMARY KEY,
    treatment_order_id BIGINT NOT NULL REFERENCES his_treatment_orders(id) ON DELETE CASCADE,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    executor_id BIGINT,
    note TEXT NOT NULL DEFAULT '',
    patient_signature_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_treatment_executions_order ON his_treatment_executions(treatment_order_id);

-- 2.5 收费域（原子 + 打包）

CREATE TABLE IF NOT EXISTS his_orders (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT NOT NULL,
    encounter_id BIGINT,
    order_no TEXT NOT NULL,
    items_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    discount_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    payable_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    paid_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending', -- pending/partial/paid/refunded/void
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, order_no)
);
CREATE INDEX IF NOT EXISTS idx_his_orders_query ON his_orders(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_his_orders_customer ON his_orders(tenant_id, customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS his_order_items (
    id BIGSERIAL PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES his_orders(id) ON DELETE CASCADE,
    item_type TEXT NOT NULL, -- charge_item/bundle/drug
    ref_id BIGINT,
    prescription_id BIGINT, -- drug 行关联处方：收费后处方 charged，退费回冲 issued
    name TEXT NOT NULL,
    unit_price NUMERIC(12,2) NOT NULL DEFAULT 0,
    quantity NUMERIC(12,2) NOT NULL DEFAULT 1,
    line_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    discount_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    payable NUMERIC(12,2) NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_his_order_items_order ON his_order_items(order_id);

CREATE TABLE IF NOT EXISTS his_payments (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    order_id BIGINT NOT NULL REFERENCES his_orders(id),
    method TEXT NOT NULL DEFAULT 'cash', -- cash/wechat/alipay/insurance(预留)
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'paid',
    paid_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    operator_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_payments_order ON his_payments(order_id);

CREATE TABLE IF NOT EXISTS his_refunds (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    order_id BIGINT NOT NULL REFERENCES his_orders(id),
    payment_id BIGINT REFERENCES his_payments(id),
    amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'done',
    operator_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_refunds_order ON his_refunds(order_id);

-- 2.6 药房域

CREATE TABLE IF NOT EXISTS his_stock (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    drug_id BIGINT NOT NULL REFERENCES his_drugs(id),
    batch_no TEXT NOT NULL DEFAULT '',
    quantity NUMERIC(12,2) NOT NULL DEFAULT 0,
    expiry_date DATE,
    location TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_stock_drug ON his_stock(tenant_id, drug_id);

CREATE TABLE IF NOT EXISTS his_stock_movements (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    drug_id BIGINT NOT NULL REFERENCES his_drugs(id),
    move_type TEXT NOT NULL, -- inbound/dispense/return/check/loss
    quantity NUMERIC(12,2) NOT NULL DEFAULT 0,
    batch_no TEXT NOT NULL DEFAULT '',
    ref_type TEXT NOT NULL DEFAULT '',
    ref_id BIGINT,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_stock_movements_drug ON his_stock_movements(tenant_id, drug_id, created_at DESC);

-- 2.7 文书、签名与打印域（统一文书服务）

CREATE TABLE IF NOT EXISTS his_documents (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    customer_id BIGINT,
    encounter_id BIGINT,
    doc_type TEXT NOT NULL, -- 门诊病历首页/初诊病历/复诊病历/处方/检查申请/处置单/治疗执行记录/随访记录/知情同意/不良事件/转诊
    ref_id BIGINT,
    content_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft', -- draft/signed/archived/printed
    printed_at TIMESTAMPTZ,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_his_documents_query ON his_documents(tenant_id, customer_id, doc_type, status);
CREATE INDEX IF NOT EXISTS idx_his_documents_ref ON his_documents(doc_type, ref_id);

CREATE TABLE IF NOT EXISTS his_signatures (
    id BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES his_documents(id) ON DELETE CASCADE,
    signer_type TEXT NOT NULL, -- patient/agent/doctor/pharmacist/nurse
    signer_customer_id BIGINT,
    signer_employee_id BIGINT,
    signature_path TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL,
    signed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    device_ip TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT 'local_pad' -- local_pad/ca
);
CREATE INDEX IF NOT EXISTS idx_his_signatures_document ON his_signatures(document_id);

-- 随访文书补录字段（外接随访产品，不动随访表）
CREATE TABLE IF NOT EXISTS his_followup_doc_extensions (
    id BIGSERIAL PRIMARY KEY,
    followup_id BIGINT NOT NULL,
    symptoms TEXT NOT NULL DEFAULT '',
    adherence TEXT NOT NULL DEFAULT '', -- 良好/一般/差
    adverse_reactions TEXT NOT NULL DEFAULT '',
    education TEXT NOT NULL DEFAULT '',
    next_followup_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (followup_id)
);

-- 2.8 开放接口域

CREATE TABLE IF NOT EXISTS his_api_clients (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    name TEXT NOT NULL,
    api_key_hash TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT 'read',
    status TEXT NOT NULL DEFAULT 'active',
    rate_limit INT NOT NULL DEFAULT 60, -- 每分钟
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);
