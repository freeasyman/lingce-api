-- 原始录音播放授权名单：机构管理员按「录音归属人 → 可听人」交叉打勾。
-- 本人与管理员天然可播，不需要在此表打勾；此表只描述"谁授权谁听别人的录音"。

CREATE TABLE IF NOT EXISTS recording_playback_listeners (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
  owner_employee_id BIGINT NOT NULL CHECK (owner_employee_id > 0),
  listener_employee_id BIGINT NOT NULL CHECK (listener_employee_id > 0),
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_by BIGINT,
  updated_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, owner_employee_id, listener_employee_id),
  CHECK (owner_employee_id <> listener_employee_id)
);

CREATE INDEX IF NOT EXISTS idx_recording_playback_listeners_owner
  ON recording_playback_listeners (tenant_id, owner_employee_id, is_active);
CREATE INDEX IF NOT EXISTS idx_recording_playback_listeners_listener
  ON recording_playback_listeners (tenant_id, listener_employee_id, is_active);
