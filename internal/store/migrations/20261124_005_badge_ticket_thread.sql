-- 工牌工单：回复客户的消息线程 + 换货留痕（换出的设备号、物流单号）。
CREATE TABLE IF NOT EXISTS badge_ticket_messages (
    id          BIGSERIAL PRIMARY KEY,
    ticket_id   BIGINT NOT NULL REFERENCES badge_tickets(id) ON DELETE CASCADE,
    author_type TEXT   NOT NULL,                 -- ops / customer
    author_id   BIGINT,
    author_name TEXT   NOT NULL DEFAULT '',
    body        TEXT   NOT NULL,
    emailed_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_badge_ticket_messages_ticket ON badge_ticket_messages (ticket_id, id);

ALTER TABLE badge_tickets
    ADD COLUMN IF NOT EXISTS resolution_type       TEXT,   -- our_issue / customer_issue / other
    ADD COLUMN IF NOT EXISTS swap_device_id        BIGINT,
    ADD COLUMN IF NOT EXISTS swap_device_no        TEXT,
    ADD COLUMN IF NOT EXISTS outbound_tracking_no  TEXT,
    ADD COLUMN IF NOT EXISTS inbound_tracking_no   TEXT,
    ADD COLUMN IF NOT EXISTS shipped_at            TIMESTAMPTZ;
