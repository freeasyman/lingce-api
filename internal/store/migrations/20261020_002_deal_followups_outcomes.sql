-- 赢客专家：销售跟进与经营结果事实。
-- 边界：
--   1) 跟进是销售任务，和随访中心的医疗任务分开存储、分开枚举；
--   2) 结果事件只追加不覆盖，历史状态不可改写；
--   3) 金额只记录事实（预约/定金/付款/退款），机会金额不落库。

CREATE TABLE IF NOT EXISTS deal_follow_ups (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  encounter_id BIGINT NOT NULL,
  recording_id BIGINT NOT NULL,
  customer_id BIGINT,
  customer_name TEXT NOT NULL DEFAULT '',
  objective TEXT NOT NULL,
  action TEXT NOT NULL,
  suggested_content TEXT NOT NULL DEFAULT '',
  owner_employee_id BIGINT,
  owner_name TEXT NOT NULL DEFAULT '',
  due_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT '待处理',
  result TEXT NOT NULL DEFAULT '',
  result_note TEXT NOT NULL DEFAULT '',
  next_action TEXT NOT NULL DEFAULT '',
  created_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  closed_at TIMESTAMPTZ
);

-- 一条接待同一时间只允许一个活动任务；完成后可再建，历史保留。
CREATE UNIQUE INDEX IF NOT EXISTS deal_follow_ups_one_active_per_encounter
  ON deal_follow_ups (tenant_id, encounter_id)
  WHERE status IN ('待处理', '处理中', '等待客户', '等待内部协作', '暂停联系');

CREATE INDEX IF NOT EXISTS idx_deal_follow_ups_tenant_status
  ON deal_follow_ups (tenant_id, status, due_at);

CREATE INDEX IF NOT EXISTS idx_deal_follow_ups_tenant_encounter
  ON deal_follow_ups (tenant_id, encounter_id);

CREATE TABLE IF NOT EXISTS deal_follow_up_events (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  follow_up_id BIGINT NOT NULL REFERENCES deal_follow_ups (id) ON DELETE CASCADE,
  from_status TEXT NOT NULL DEFAULT '',
  to_status TEXT NOT NULL DEFAULT '',
  result TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  actor_employee_id BIGINT,
  actor_name TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deal_follow_up_events_follow_up
  ON deal_follow_up_events (tenant_id, follow_up_id, created_at);

CREATE TABLE IF NOT EXISTS deal_outcome_events (
  id BIGSERIAL PRIMARY KEY,
  tenant_id BIGINT NOT NULL,
  encounter_id BIGINT NOT NULL,
  recording_id BIGINT NOT NULL,
  type TEXT NOT NULL,
  amount NUMERIC(12, 2),
  occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  source TEXT NOT NULL DEFAULT 'employee',
  source_ref TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  confirmed_by BIGINT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_deal_outcome_events_tenant_encounter
  ON deal_outcome_events (tenant_id, encounter_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_deal_outcome_events_tenant_type
  ON deal_outcome_events (tenant_id, type, occurred_at DESC);
