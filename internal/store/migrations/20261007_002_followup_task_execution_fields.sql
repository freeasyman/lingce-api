-- 随访任务质量、患者识别和执行闭环字段。
ALTER TABLE IF EXISTS recording_tasks
  ADD COLUMN IF NOT EXISTS task_category VARCHAR(64),
  ADD COLUMN IF NOT EXISTS anonymous_patient BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS patient_age INTEGER,
  ADD COLUMN IF NOT EXISTS patient_gender VARCHAR(32),
  ADD COLUMN IF NOT EXISTS phone_script TEXT,
  ADD COLUMN IF NOT EXISTS wechat_content TEXT,
  ADD COLUMN IF NOT EXISTS execution_result VARCHAR(64),
  ADD COLUMN IF NOT EXISTS execution_difficulty TEXT,
  ADD COLUMN IF NOT EXISTS doctor_escalation TEXT,
  ADD COLUMN IF NOT EXISTS next_contact_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_recording_tasks_followup_anonymous
  ON recording_tasks (tenant_id, anonymous_patient, status);
