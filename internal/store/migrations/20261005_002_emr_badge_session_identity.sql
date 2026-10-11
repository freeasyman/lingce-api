-- 为工牌会话增加稳定的录音关联标识；不修改旧 Worker 或工牌网关。
ALTER TABLE emr_badge_sessions ADD COLUMN IF NOT EXISTS order_no VARCHAR(255);
ALTER TABLE emr_badge_sessions ADD COLUMN IF NOT EXISTS state VARCHAR(32) NOT NULL DEFAULT 'recording';

UPDATE emr_badge_sessions
SET order_no = 'legacy-badge:' || id::text
WHERE order_no IS NULL OR order_no = '';

UPDATE emr_badge_sessions
SET state = CASE
    WHEN recording_id IS NOT NULL THEN 'uploaded'
    WHEN stopped_at IS NOT NULL THEN 'waiting_upload'
    ELSE 'recording'
END;

ALTER TABLE emr_badge_sessions ALTER COLUMN order_no SET NOT NULL;
ALTER TABLE emr_badge_sessions DROP CONSTRAINT IF EXISTS emr_badge_sessions_status_check;
ALTER TABLE emr_badge_sessions DROP CONSTRAINT IF EXISTS emr_badge_sessions_state_check;
ALTER TABLE emr_badge_sessions ADD CONSTRAINT emr_badge_sessions_state_check
    CHECK (state IN ('recording', 'waiting_upload', 'uploaded', 'failed'));

CREATE UNIQUE INDEX IF NOT EXISTS uk_emr_badge_sessions_order_no
    ON emr_badge_sessions (tenant_id, order_no);

DROP INDEX IF EXISTS uk_emr_badge_sessions_active_employee;
CREATE UNIQUE INDEX IF NOT EXISTS uk_emr_badge_sessions_active_employee
    ON emr_badge_sessions (tenant_id, employee_id)
    WHERE state = 'recording';
