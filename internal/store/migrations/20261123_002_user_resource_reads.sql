-- 通用「已读」记录：菜单角标只统计当前用户未看过的待办。
-- 资源类型见 internal/readmark：emr_record / followup_task / content_insight / compliance_finding / compliance_person / patient_screenshot。
CREATE TABLE IF NOT EXISTS user_resource_reads (
    tenant_id     BIGINT      NOT NULL,
    employee_id   BIGINT      NOT NULL,
    resource_type TEXT        NOT NULL,
    resource_id   BIGINT      NOT NULL,
    read_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, employee_id, resource_type, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_user_resource_reads_lookup
    ON user_resource_reads (tenant_id, employee_id, resource_type);
