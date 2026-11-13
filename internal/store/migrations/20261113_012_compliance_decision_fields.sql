-- +goose Up
-- 核查事项处理决定 / 签收 / 复查 / 关闭的正式落库字段。
-- 页面把处理决定分成「关闭（误报）/ 发改进意见书 / 立项整改」三分法，
-- 与结论枚举 confirmed / not_confirmed / unable_to_determine 不是一一对应，
-- 因此单独记录 decision_action，结论仍写 conclusion 以保持既有查询兼容。
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS decision_action VARCHAR(16);
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS decision_reason TEXT;
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS decision_by BIGINT;
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS decision_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS sign_method VARCHAR(32);
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS signed_at TIMESTAMPTZ;
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS review_date DATE;
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS review_method VARCHAR(64);
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS review_conclusion VARCHAR(32);
ALTER TABLE IF EXISTS compliance_check_items ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS compliance_check_items_tenant_decision_idx
  ON compliance_check_items (tenant_id, decision_action, closed_at);

-- +goose Down
DROP INDEX IF EXISTS compliance_check_items_tenant_decision_idx;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS closed_at;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS review_conclusion;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS review_method;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS review_date;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS signed_at;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS sign_method;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS decision_at;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS decision_by;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS decision_reason;
ALTER TABLE IF EXISTS compliance_check_items DROP COLUMN IF EXISTS decision_action;
