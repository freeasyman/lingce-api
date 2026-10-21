ALTER TABLE content_accounts
    ADD COLUMN IF NOT EXISTS monthly_target INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS content_plan_items (
    id BIGSERIAL PRIMARY KEY,
    tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id BIGINT NOT NULL REFERENCES content_accounts(id) ON DELETE CASCADE,
    column_id BIGINT REFERENCES content_account_columns(id) ON DELETE SET NULL,
    column_name_snapshot VARCHAR(200) NOT NULL DEFAULT '',
    month_start DATE NOT NULL,
    planned_date DATE,
    title VARCHAR(300) NOT NULL DEFAULT '',
    topic_type VARCHAR(30) NOT NULL DEFAULT 'manual',
    topic_id BIGINT,
    status VARCHAR(30) NOT NULL DEFAULT 'todo',
    content_id BIGINT,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_by BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_content_plan_items_content ON content_plan_items (content_id) WHERE content_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_content_plan_items_month ON content_plan_items (tenant_id, month_start, account_id);
CREATE INDEX IF NOT EXISTS idx_content_plan_items_week ON content_plan_items (tenant_id, planned_date);

INSERT INTO content_plan_items (tenant_id, account_id, column_id, column_name_snapshot, month_start, planned_date, title, topic_type, topic_id, status, content_id, created_at, updated_at)
SELECT ci.tenant_id, s.account_id, s.column_id, c.name,
       date_trunc('month', s.week_start)::date, NULL, ci.title, 'legacy',
       NULL, CASE WHEN ci.status = 'published' THEN 'published' ELSE 'drafting' END,
       ci.id, ci.created_at, ci.updated_at
FROM content_items ci
JOIN content_plan_slots s ON s.id = (ci.extra_data->>'plan_slot_id')::bigint AND s.tenant_id = ci.tenant_id
JOIN content_account_columns c ON c.id = s.column_id
WHERE ci.deleted_at IS NULL
  AND (ci.extra_data->>'plan_slot_id') ~ '^[0-9]+$'
  AND NOT EXISTS (SELECT 1 FROM content_plan_items p WHERE p.content_id = ci.id);
