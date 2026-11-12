-- 检验（LIS）：申请单 + 结果项。设计基线 docs/his/15-检验检查数据模型.md。
-- 只新增表，不改共享表；申请单号复用 his_number_counters（scope='lab'，L+日期+4位）。

CREATE TABLE IF NOT EXISTS his_lab_requests (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    encounter_id BIGINT NOT NULL REFERENCES encounters(id),
    customer_id BIGINT NOT NULL,
    request_no TEXT NOT NULL,               -- L+日期+4位
    item_name TEXT NOT NULL,                -- 血常规/白带常规…
    specimen_type TEXT NOT NULL DEFAULT '', -- 血/尿/分泌物
    purpose TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending', -- pending/sampled/reported/cancelled
    sampled_at TIMESTAMPTZ,
    reported_at TIMESTAMPTZ,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, request_no),
    CONSTRAINT his_lab_requests_status_check
        CHECK (status IN ('pending', 'sampled', 'reported', 'cancelled'))
);

-- 检验工作站：租户 + 页签状态 + 开单时间倒序。
CREATE INDEX IF NOT EXISTS idx_his_lab_requests_station
    ON his_lab_requests(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_his_lab_requests_encounter
    ON his_lab_requests(tenant_id, encounter_id);

CREATE TABLE IF NOT EXISTS his_lab_result_items (
    id BIGSERIAL PRIMARY KEY,
    request_id BIGINT NOT NULL REFERENCES his_lab_requests(id) ON DELETE CASCADE,
    item_code TEXT NOT NULL DEFAULT '',
    item_name TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '',
    unit TEXT NOT NULL DEFAULT '',
    reference_range TEXT NOT NULL DEFAULT '',
    flag TEXT NOT NULL DEFAULT ''           -- H 高 / L 低 / '' 正常
);
CREATE INDEX IF NOT EXISTS idx_his_lab_result_items_request
    ON his_lab_result_items(request_id);
