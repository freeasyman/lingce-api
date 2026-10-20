-- 赢客专家：录音事实补退款金额。
-- 目的：实际净成交额 = 成交金额 - 退款金额，退款必须有独立事实字段，
-- 不能靠成交状态推断，也不能把毛成交当成净成交。
ALTER TABLE recordings
  ADD COLUMN IF NOT EXISTS refund_amount NUMERIC(12, 2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN recordings.refund_amount IS '退款金额；与 converted_amount 一起构成实际净成交额，仅用于经营统计口径';
