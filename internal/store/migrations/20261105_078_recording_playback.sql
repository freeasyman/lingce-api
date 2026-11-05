-- 原始录音播放：机构级开关与窗口、播放留痕、窗口外审批调取授权（限时）。
-- 默认不建机构行时按「开启 + 90 天」生效；敏感机构可写入关闭或 0 天。

CREATE TABLE IF NOT EXISTS recording_playback_configs (
  tenant_id BIGINT PRIMARY KEY CHECK (tenant_id > 0),
  playback_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  playback_window_days INTEGER NOT NULL DEFAULT 90 CHECK (playback_window_days >= 0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 每次成功生成播放地址都写一条，作为「谁能听、何时听」的留痕。
CREATE TABLE IF NOT EXISTS recording_play_logs (
  id BIGSERIAL PRIMARY KEY,
  recording_id BIGINT NOT NULL CHECK (recording_id > 0),
  tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
  employee_id BIGINT NOT NULL CHECK (employee_id > 0),
  reason TEXT NOT NULL DEFAULT 'review',
  authorizer_id BIGINT,
  played_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_recording_play_logs_recording ON recording_play_logs (recording_id, played_at DESC);
CREATE INDEX IF NOT EXISTS idx_recording_play_logs_tenant ON recording_play_logs (tenant_id, played_at DESC);

-- 窗口外/跨人调取的限时授权（轻量版：申请人填理由即放行，authorizer=requester）。
CREATE TABLE IF NOT EXISTS recording_playback_grants (
  id BIGSERIAL PRIMARY KEY,
  recording_id BIGINT NOT NULL CHECK (recording_id > 0),
  tenant_id BIGINT NOT NULL CHECK (tenant_id > 0),
  requester_id BIGINT NOT NULL CHECK (requester_id > 0),
  authorizer_id BIGINT NOT NULL CHECK (authorizer_id > 0),
  reason TEXT NOT NULL,
  granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_recording_playback_grants_active
  ON recording_playback_grants (recording_id, requester_id, expires_at DESC);
