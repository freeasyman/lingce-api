-- 演示与列表页读路径索引：只加组合索引，不改业务语义。
CREATE INDEX IF NOT EXISTS idx_recordings_tenant_deleted_scope_recorded
  ON recordings (tenant_id, deleted_at, lower(COALESCE(business_scope, '')), recorded_at DESC);

CREATE INDEX IF NOT EXISTS idx_deal_outcome_tenant_encounter_occurred
  ON deal_outcome_events (tenant_id, encounter_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_compliance_items_tenant_status_updated
  ON compliance_check_items (tenant_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_customers_tenant_deleted_created
  ON customers (tenant_id, deleted_at, created_at DESC);
