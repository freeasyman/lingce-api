-- EMR owns only delivery for realtime recordings that skip Worker.
-- Accepted requests are terminal; followup owns subsequent generation retries.
CREATE TABLE IF NOT EXISTS emr_followup_deliveries (
    tenant_id BIGINT NOT NULL,
    recording_id BIGINT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted')),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    request_id TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, recording_id)
);
CREATE INDEX IF NOT EXISTS idx_emr_followup_delivery_pending
    ON emr_followup_deliveries(next_attempt_at) WHERE status='pending';
