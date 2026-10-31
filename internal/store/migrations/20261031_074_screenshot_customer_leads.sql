-- Automatic screenshot leads stay separate from human identity confirmation.
ALTER TABLE patient_conversation_sources
  DROP CONSTRAINT IF EXISTS patient_conversation_sources_match_status_check;
ALTER TABLE patient_conversation_sources
  ADD CONSTRAINT patient_conversation_sources_match_status_check
  CHECK (match_status IN ('pending','pending_confirm','confirmed','rejected'));
ALTER TABLE patient_conversation_sources
  ADD COLUMN IF NOT EXISTS auto_created_customer_id BIGINT REFERENCES customers(id);
CREATE INDEX IF NOT EXISTS idx_screenshot_pending_confirmation
  ON patient_conversation_sources(tenant_id,captured_at DESC,id DESC)
  WHERE source_type='screenshot' AND match_status IN ('pending','pending_confirm');
CREATE TABLE IF NOT EXISTS screenshot_customer_reviews (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  source_id BIGINT NOT NULL REFERENCES patient_conversation_sources(id),
  action TEXT NOT NULL CHECK (action IN ('confirm','merge','rebind','reject','backfill')),
  from_customer_id BIGINT,
  to_customer_id BIGINT,
  operator_id BIGINT NOT NULL,
  detail JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_screenshot_customer_reviews_source
  ON screenshot_customer_reviews(tenant_id,source_id,id);

-- Apply the same admission rule to every formal task/plan writer.
CREATE OR REPLACE FUNCTION guard_unconfirmed_screenshot_customer() RETURNS trigger AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM customers c WHERE c.id=NEW.customer_id AND c.tenant_id=NEW.tenant_id
      AND c.extra_data->>'screenshot_auto_lead'='true'
      AND c.extra_data->>'identity_status'='pending_confirm') THEN
    RAISE EXCEPTION '请先在待确认客户中确认线索，再进入经营计划或正式任务';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS trg_screenshot_customer_task_guard ON patient_operation_tasks;
CREATE TRIGGER trg_screenshot_customer_task_guard BEFORE INSERT OR UPDATE OF customer_id
  ON patient_operation_tasks FOR EACH ROW EXECUTE FUNCTION guard_unconfirmed_screenshot_customer();
DROP TRIGGER IF EXISTS trg_screenshot_customer_plan_guard ON patient_ops_plan_members;
CREATE TRIGGER trg_screenshot_customer_plan_guard BEFORE INSERT OR UPDATE OF customer_id
  ON patient_ops_plan_members FOR EACH ROW EXECUTE FUNCTION guard_unconfirmed_screenshot_customer();
