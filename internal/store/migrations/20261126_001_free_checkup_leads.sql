-- 免费体检（获客 H5）线索表：独立产品，不挂真实机构，run 存在保留租户 9999。
CREATE TABLE IF NOT EXISTS free_checkup_leads (
  id BIGSERIAL PRIMARY KEY,
  run_id BIGINT NOT NULL,
  org_name TEXT NOT NULL DEFAULT '',
  region TEXT NOT NULL DEFAULT '',
  question TEXT NOT NULL DEFAULT '',
  doctor_name TEXT NOT NULL DEFAULT '',
  phone TEXT NOT NULL DEFAULT '',
  scene TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'new',
  note TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_free_checkup_leads_created ON free_checkup_leads (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_free_checkup_leads_status ON free_checkup_leads (status);
