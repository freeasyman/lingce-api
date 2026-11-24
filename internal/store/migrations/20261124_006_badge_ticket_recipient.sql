-- 换新收件信息：工单上记录本次寄新机的收件人/电话/地址；机构档案补详细地址用于自动带入。
ALTER TABLE badge_tickets
    ADD COLUMN IF NOT EXISTS recipient_name    TEXT,
    ADD COLUMN IF NOT EXISTS recipient_phone   TEXT,
    ADD COLUMN IF NOT EXISTS recipient_address TEXT;

ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS address TEXT;
