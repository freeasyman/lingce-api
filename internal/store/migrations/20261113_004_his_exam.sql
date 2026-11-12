-- 检查（PACS）：检查申请单 + 检查报告。设计基线 docs/his/15-检验检查数据模型.md「二、检查（PACS）」。
-- 只新增表，不改共享表；申请单号复用 his_number_counters（scope='exam'，E+日期+4位）。

CREATE TABLE IF NOT EXISTS his_exam_requests (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id),
    encounter_id BIGINT NOT NULL REFERENCES encounters(id),
    customer_id BIGINT NOT NULL,
    request_no TEXT NOT NULL,               -- E+日期+4位
    item_name TEXT NOT NULL,                -- 妇科B超 / 胸部DR…
    body_part TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending', -- pending 待检查 / in_progress 检查中 / reported 已出报告 / cancelled 已取消
    started_at TIMESTAMPTZ,
    reported_at TIMESTAMPTZ,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, request_no),
    CONSTRAINT his_exam_requests_status_check
        CHECK (status IN ('pending', 'in_progress', 'reported', 'cancelled'))
);

-- 检查工作站：租户 + 页签状态 + 开单时间倒序。
CREATE INDEX IF NOT EXISTS idx_his_exam_requests_station
    ON his_exam_requests(tenant_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_his_exam_requests_encounter
    ON his_exam_requests(tenant_id, encounter_id);

CREATE TABLE IF NOT EXISTS his_exam_reports (
    id BIGSERIAL PRIMARY KEY,
    request_id BIGINT NOT NULL REFERENCES his_exam_requests(id) ON DELETE CASCADE,
    findings TEXT NOT NULL DEFAULT '',     -- 所见
    impression TEXT NOT NULL DEFAULT '',   -- 印象
    suggestion TEXT NOT NULL DEFAULT '',   -- 建议
    image_refs JSONB NOT NULL DEFAULT '[]',-- DICOM/影像地址（PACS 回传）
    reported_by BIGINT,
    reported_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_his_exam_reports_request
    ON his_exam_reports(request_id);
