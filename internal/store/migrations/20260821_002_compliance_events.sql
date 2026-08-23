CREATE TABLE IF NOT EXISTS compliance_events (
  id TEXT PRIMARY KEY,
  tenant_id BIGINT NOT NULL DEFAULT 0,
  encounter_id BIGINT NOT NULL,
  source VARCHAR(32) NOT NULL,
  severity VARCHAR(16) NOT NULL,
  type VARCHAR(128) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  timestamp TIMESTAMPTZ NOT NULL,
  employee_name VARCHAR(128) NOT NULL DEFAULT '',
  employee_role VARCHAR(64) NOT NULL DEFAULT '',
  department VARCHAR(128) NOT NULL DEFAULT '',
  patient_name VARCHAR(128) NOT NULL DEFAULT '',
  patient_meta VARCHAR(256) NOT NULL DEFAULT '',
  content_title VARCHAR(256) NOT NULL DEFAULT '',
  content_type VARCHAR(32) NOT NULL DEFAULT '',
  quote TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  advice TEXT NOT NULL DEFAULT '',
  legal_basis TEXT NOT NULL DEFAULT '',
  evidence_at VARCHAR(32) NOT NULL DEFAULT '',
  transcript JSONB NOT NULL DEFAULT '[]'::jsonb,
  related_actions JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ NULL,
  CONSTRAINT compliance_events_source_check CHECK (source IN ('communication', 'content')),
  CONSTRAINT compliance_events_severity_check CHECK (severity IN ('critical', 'high', 'medium', 'low', 'safe')),
  CONSTRAINT compliance_events_status_check CHECK (status IN ('pending', 'processing', 'resolved', 'ignored'))
);

CREATE INDEX IF NOT EXISTS idx_compliance_events_tenant_timestamp
  ON compliance_events (tenant_id, timestamp DESC)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_events_tenant_severity
  ON compliance_events (tenant_id, severity, timestamp DESC)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_compliance_events_tenant_encounter
  ON compliance_events (tenant_id, encounter_id)
  WHERE deleted_at IS NULL;

INSERT INTO compliance_events (
  id, tenant_id, encounter_id, source, severity, type, status, timestamp,
  employee_name, employee_role, department, patient_name, patient_meta,
  content_title, content_type, quote, summary, advice, legal_basis, evidence_at,
  transcript, related_actions
) VALUES (
  'evt-001', 0, 1, 'communication', 'critical', '保证疗效', 'pending', '2026-08-20T09:23:00+08:00',
  '张医生', '医生', '口腔种植科', '王女士', '35岁 · 种植牙咨询',
  '', '', '我保证你一定能治好。', '对话中出现明确保证疗效表述，存在较高投诉与纠纷风险。',
  '建议立即由院长或合规专员介入，并在病历中补充风险告知。', '《医疗广告管理办法》第七条', '09:23',
  '[{"time":"08:45","speaker":"患者","text":"医生，我这个能治好吗？"},{"time":"09:23","speaker":"张医生","text":"我保证你一定能治好。","violation":true}]'::jsonb,
  '["查看详情","导出证据包","标记处理中"]'::jsonb
) ON CONFLICT (id) DO NOTHING;

INSERT INTO compliance_events (
  id, tenant_id, encounter_id, source, severity, type, status, timestamp,
  employee_name, employee_role, department, patient_name, patient_meta,
  content_title, content_type, quote, summary, advice, legal_basis, evidence_at,
  transcript, related_actions
) VALUES (
  'evt-002', 0, 2, 'communication', 'critical', '制造焦虑', 'processing', '2026-08-20T10:47:00+08:00',
  '李咨询师', '咨询师', '咨询中心', '赵先生', '42岁 · 近视手术咨询',
  '', '', '你再不做，度数继续涨，上了年纪就来不及了。', '通过制造紧迫感推动成交，且未提供个体化评估依据。',
  '建议统一改成“是否适合、何时做需要根据检查结果判断”。', '医疗服务宣传与消费者权益保护相关要求', '10:47',
  '[{"time":"10:31","speaker":"赵先生","text":"我还想再考虑一下。"},{"time":"10:47","speaker":"李咨询师","text":"你再不做，度数继续涨，上了年纪就来不及了。","violation":true}]'::jsonb,
  '["查看详情","导出证据包","追加沟通建议"]'::jsonb
) ON CONFLICT (id) DO NOTHING;
