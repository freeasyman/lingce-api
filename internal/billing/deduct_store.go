package billing

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// deductTx 是 DeductTx 的 pgx 实现：所有方法共用同一个 pgx.Tx，由 service 编排提交/回滚。
type deductTx struct{ tx pgx.Tx }

var _ DeductTx = (*deductTx)(nil)

// BeginDeduct 开启扣量事务。
func (s *Store) BeginDeduct(ctx context.Context) (DeductTx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin deduct transaction: %w", err)
	}
	return &deductTx{tx: tx}, nil
}

const lockEntitlementsSQL = `
SELECT product_code,
       '' AS product_name,
       unit,
       edition,
       quota_type,
       granted::text,
       used::text,
       rollover_bal::text,
       balance::text,
       to_char(period_end, 'YYYY-MM-DD'),
       status
FROM product_entitlements
WHERE tenant_id = $1
  AND product_code = $2
  AND unit = ANY($3::text[])
ORDER BY unit, quota_type, edition
FOR UPDATE`

// LockEntitlements 对涉及的单位加行锁，返回扣量前的额度行。
//
// 排序 (unit, quota_type, edition) 后再 FOR UPDATE，保证多个扣量事务以相同顺序拿锁，
// 避免相互等待成环；锁住后同一单位的并发扣量只能串行执行。
func (t *deductTx) LockEntitlements(ctx context.Context, tenantID int64, productCode string, units []string) ([]Entitlement, error) {
	rows, err := t.tx.Query(ctx, lockEntitlementsSQL, tenantID, productCode, units)
	if err != nil {
		return nil, fmt.Errorf("query entitlements for update: %w", err)
	}
	defer rows.Close()

	items := make([]Entitlement, 0, len(units)*2)
	for rows.Next() {
		item, err := scanEntitlementRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan entitlements for update: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entitlements for update: %w", err)
	}
	return items, nil
}

const idempotencyKeyExistsSQL = `
SELECT EXISTS (
    SELECT 1
    FROM entitlement_ledger
    WHERE tenant_id = $1
      AND idempotency_key = $2
)`

// IdempotencyKeyExists 查询幂等键是否已经落过流水。
//
// 这是"幂等优先于额度校验"的前置判断；真正的并发防重仍由唯一索引保证：
// 两个事务同时通过这里时，后插入的一方会拿到 23505 并返回 ErrAlreadyDeducted。
func (t *deductTx) IdempotencyKeyExists(ctx context.Context, tenantID int64, idempotencyKey string) (bool, error) {
	var exists bool
	if err := t.tx.QueryRow(ctx, idempotencyKeyExistsSQL, tenantID, idempotencyKey).Scan(&exists); err != nil {
		return false, fmt.Errorf("query entitlement ledger idempotency key: %w", err)
	}
	return exists, nil
}

const insertUsageLedgerSQL = `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, idempotency_key, operator)
VALUES ($1, $2, $3, 'usage', $4::numeric, $5, $6, $7, $8)`

// InsertUsageLedger 写入一条 usage 流水。
//
// 幂等键只出现在本次扣量的第一条流水上，其余为 NULL；部分唯一索引
// entitlement_ledger_idempotency_uq 只约束非 NULL 值，因此同一 key 第二次插入会
// 报 SQLSTATE 23505，这里转换成 ErrAlreadyDeducted。
func (t *deductTx) InsertUsageLedger(ctx context.Context, entry DeductLedgerEntry) error {
	var idempotencyKey any
	if entry.IdempotencyKey != nil {
		if key := strings.TrimSpace(*entry.IdempotencyKey); key != "" {
			idempotencyKey = key
		}
	}
	_, err := t.tx.Exec(ctx, insertUsageLedgerSQL,
		entry.TenantID,
		entry.ProductCode,
		entry.Unit,
		entry.Amount.String(),
		entry.Source,
		entry.Reason,
		idempotencyKey,
		entry.Operator,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyDeducted
		}
		return fmt.Errorf("insert usage ledger: %w", err)
	}
	return nil
}

const applyEntitlementChangeSQL = `
UPDATE product_entitlements
SET rollover_bal = $6::numeric,
    used = $7::numeric,
    -- paused/active 互相切换；expired/cleared 属于人工/到期状态，扣量不复活它们。
    status = CASE WHEN status IN ('active', 'paused') THEN $8 ELSE status END,
    updated_at = NOW()
WHERE tenant_id = $1
  AND product_code = $2
  AND unit = $3
  AND edition = $4
  AND quota_type = $5`

// ApplyEntitlementChanges 按行写回 rollover_bal / used / status。
//
// balance 不在这里写：RecomputeAndList 会在同一事务里用 SQL 按
// rollover_bal + granted - used 重算，避免 Go 侧与库里的口径漂移。
func (t *deductTx) ApplyEntitlementChanges(ctx context.Context, tenantID int64, productCode string, changes []DeductChange) error {
	for _, change := range changes {
		tag, err := t.tx.Exec(ctx, applyEntitlementChangeSQL,
			tenantID,
			productCode,
			change.Unit,
			change.Edition,
			change.QuotaType,
			change.RolloverBal.String(),
			change.Used.String(),
			change.Status,
		)
		if err != nil {
			return fmt.Errorf("update entitlement %s/%s/%s: %w", change.Unit, change.Edition, change.QuotaType, err)
		}
		if tag.RowsAffected() == 0 {
			// 行在锁定时一定存在；影响 0 行说明数据被并发删除或唯一键与预期不一致，必须回滚。
			return fmt.Errorf("update entitlement %s/%s/%s: row not found", change.Unit, change.Edition, change.QuotaType)
		}
	}
	return nil
}

const recomputeBalancesSQL = `
UPDATE product_entitlements
SET balance = rollover_bal + granted - used,
    updated_at = NOW()
WHERE tenant_id = $1
  AND product_code = $2
  AND unit = ANY($3::text[])`

const listProductEntitlementsSQL = `
SELECT e.product_code,
       COALESCE(pc.product_name, ''),
       e.unit,
       e.edition,
       e.quota_type,
       e.granted::text,
       e.used::text,
       e.rollover_bal::text,
       e.balance::text,
       to_char(e.period_end, 'YYYY-MM-DD'),
       e.status
FROM product_entitlements e
LEFT JOIN LATERAL (
    SELECT c.product_name
      FROM product_catalog c
     WHERE c.product_code = e.product_code
       AND c.product_name <> ''
     ORDER BY c.price_version DESC, c.id DESC
     LIMIT 1
) pc ON TRUE
WHERE e.tenant_id = $1
  AND e.product_code = $2
ORDER BY e.unit, e.quota_type, e.edition`

// RecomputeAndList 先用 SQL 重算涉及单位的 balance，再读回该产品所有单位的额度。
//
// 重算口径固定为 rollover_bal + granted - used，和表结构约定一致；
// 提交前读回保证接口返回的余额就是落库后的余额。
func (t *deductTx) RecomputeAndList(ctx context.Context, tenantID int64, productCode string, units []string) ([]Entitlement, error) {
	if _, err := t.tx.Exec(ctx, recomputeBalancesSQL, tenantID, productCode, units); err != nil {
		return nil, fmt.Errorf("recompute entitlement balances: %w", err)
	}

	rows, err := t.tx.Query(ctx, listProductEntitlementsSQL, tenantID, productCode)
	if err != nil {
		return nil, fmt.Errorf("query product entitlements after deduct: %w", err)
	}
	defer rows.Close()

	items := make([]Entitlement, 0, len(units)*2)
	for rows.Next() {
		item, err := scanEntitlementRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan product entitlements after deduct: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate product entitlements after deduct: %w", err)
	}
	return items, nil
}

// Commit 提交扣量事务。
func (t *deductTx) Commit(ctx context.Context) error { return t.tx.Commit(ctx) }

// Rollback 回滚扣量事务；已提交后调用返回 pgx.ErrTxClosed，由 service 忽略。
func (t *deductTx) Rollback(ctx context.Context) error { return t.tx.Rollback(ctx) }

// scanEntitlementRow 解析额度行（列顺序与 lockEntitlementsSQL / listProductEntitlementsSQL 一致）。
func scanEntitlementRow(scan func(dest ...any) error) (Entitlement, error) {
	var (
		item        Entitlement
		granted     string
		used        string
		rolloverBal string
		balance     string
	)
	if err := scan(
		&item.ProductCode,
		&item.ProductName,
		&item.Unit,
		&item.Edition,
		&item.QuotaType,
		&granted,
		&used,
		&rolloverBal,
		&balance,
		&item.PeriodEnd,
		&item.Status,
	); err != nil {
		return Entitlement{}, err
	}
	item.Granted = Decimal(granted)
	item.Used = Decimal(used)
	item.RolloverBal = Decimal(rolloverBal)
	item.Balance = Decimal(balance)
	return item, nil
}
