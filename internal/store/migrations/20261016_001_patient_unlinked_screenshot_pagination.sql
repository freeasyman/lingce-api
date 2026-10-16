CREATE INDEX IF NOT EXISTS idx_patient_unlinked_screenshot_time
ON patient_conversation_sources (tenant_id, captured_at DESC, id DESC)
WHERE customer_id IS NULL AND source_type = 'screenshot';
