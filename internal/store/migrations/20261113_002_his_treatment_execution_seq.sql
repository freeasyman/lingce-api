-- 治疗执行序号：执行站按「第 N 次」展示执行历史。
-- 081 建表时 his_treatment_executions 只有流水 id，没有业务序号；
-- 这里补 seq 并按历史执行时间回填，再加唯一约束保证同一医嘱内序号不重复。
ALTER TABLE his_treatment_executions ADD COLUMN IF NOT EXISTS seq INT NOT NULL DEFAULT 0;

WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY treatment_order_id ORDER BY executed_at, id) AS rn
    FROM his_treatment_executions
)
UPDATE his_treatment_executions ex
SET seq = ranked.rn
FROM ranked
WHERE ex.id = ranked.id AND ex.seq = 0;

CREATE UNIQUE INDEX IF NOT EXISTS uq_his_treatment_executions_order_seq
    ON his_treatment_executions(treatment_order_id, seq);

-- 执行站按租户 + 状态 + 时间筛选。
CREATE INDEX IF NOT EXISTS idx_his_treatment_orders_station
    ON his_treatment_orders(tenant_id, status, created_at DESC);
