package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

// GetSubscriptionState 返回机构的服务闸门状态：暂停优先，其次到期，其余返回空串。
//
// 与机构详情的 status 口径保持一致（暂停 > 未开通 > 已到期）：
//   - 暂停：tenants.paused_at 非空；
//   - 到期：当前服务期已过期（到期任务每天只跑一次，白天跨过到期日也要挡住）。
//
// 服务期来源优先新表 subscription_periods（合同/试用，覆盖今天即有效）；
// 只有当机构没有任何合同时，才回退旧的 tenant_subscriptions（老产品客户）。
// 两套栈迁移后这里曾只读旧表，导致有试用合同的老客户被判「已到期」。
//
// 找不到租户时返回空串，让后续额度查询按「没有额度」走
// （避免把不存在租户误报成服务暂停）。
func (s *Store) GetSubscriptionState(ctx context.Context, tenantID int64) (string, error) {
	var state string
	err := s.pool.QueryRow(ctx, `
SELECT CASE
           WHEN t.paused_at IS NOT NULL THEN 'paused'
           -- 新表：优先合同/试用；covers_today 为真表示今天落在某个生效合同内
           WHEN p.covers_today THEN ''
           WHEN p.id IS NOT NULL THEN 'expired'
           -- 回退旧表（没有任何合同行的老客户）
           WHEN legacy.status = 'expired' THEN 'expired'
           WHEN legacy.expired_on IS NOT NULL AND legacy.expired_on < CURRENT_DATE THEN 'expired'
           ELSE ''
       END
FROM tenants t
LEFT JOIN LATERAL (
    SELECT sp.id,
           (sp.status = 'active'
            AND sp.started_on <= CURRENT_DATE
            AND sp.expired_on >= CURRENT_DATE) AS covers_today
    FROM subscription_periods sp
    WHERE sp.tenant_id = t.id
      AND sp.status <> 'terminated'
    ORDER BY
        (sp.status = 'active' AND sp.started_on <= CURRENT_DATE AND sp.expired_on >= CURRENT_DATE) DESC,
        sp.expired_on DESC
    LIMIT 1
) p ON TRUE
LEFT JOIN LATERAL (
    -- 旧表只有老产品客户会写：取最新一条服务期。
    SELECT ts.status, ts.expired_on
    FROM tenant_subscriptions ts
    WHERE ts.tenant_id = t.id
    ORDER BY ts.created_at DESC NULLS LAST, ts.id DESC
    LIMIT 1
) legacy ON TRUE
WHERE t.id = $1
  AND t.deleted_at IS NULL`, tenantID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query subscription state: %w", err)
	}
	return state, nil
}

// subscriptionGuard 把服务闸门状态映射成扣量/预检的错误；正常状态返回 nil。
//
// 到期与暂停都必须拒绝：暂停优先（恢复后自然落到到期）。这两种错误不是
// 「计费故障」，产品端不能 fail-open，必须按客户口径拒绝本次交付。
func subscriptionGuard(state string) error {
	switch state {
	case InstitutionStatusPaused:
		return ErrSubscriptionPaused
	case InstitutionStatusExpired:
		return ErrSubscriptionExpired
	default:
		return nil
	}
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

// IsDeducted 只读查询某幂等键是否已经落过流水。
//
// 与扣量事务内的 IdempotencyKeyExists 共用同一条 SQL 与
// entitlement_ledger_idempotency_uq (tenant_id, idempotency_key) 索引，但走连接池、
// 不开启事务也不加锁：调用方只想知道"这笔交付是否已经扣过"。
func (s *Store) IsDeducted(ctx context.Context, tenantID int64, idempotencyKey string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, idempotencyKeyExistsSQL, tenantID, idempotencyKey).Scan(&exists); err != nil {
		return false, fmt.Errorf("query entitlement ledger idempotency key: %w", err)
	}
	return exists, nil
}

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

// LoadReplay 读取某幂等键对应扣量的流水金额与当前余额（只读），用于幂等命中时回放结果。
//
// 三段的原始来源拆分没有单独落库，回放时统一归到 official，并用 Replayed 标记
// 提示调用方「这是重放」。余额一律读当前真实值。
func (s *Store) LoadReplay(ctx context.Context, tenantID int64, productCode, idempotencyKey string, units []string) (*DeductResult, error) {
	rows, err := s.pool.Query(ctx, `
SELECT unit, amount
FROM entitlement_ledger
WHERE tenant_id = $1
  AND product_code = $2
  AND idempotency_key = $3
ORDER BY id`, tenantID, productCode, idempotencyKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []DeductLine
	for rows.Next() {
		var unit string
		var amount Decimal
		if err := rows.Scan(&unit, &amount); err != nil {
			return nil, err
		}
		value, err := strconv.ParseFloat(string(amount), 64)
		if err != nil {
			return nil, err
		}
		lines = append(lines, DeductLine{Unit: unit, Amount: -value, Official: 0})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, ErrAlreadyDeducted
	}

	balances, err := s.listBalances(ctx, tenantID, productCode, units)
	if err != nil {
		return nil, err
	}
	return &DeductResult{Lines: lines, Balances: balances}, nil
}

// listBalances 读当前额度行，供回放结果使用。
func (s *Store) listBalances(ctx context.Context, tenantID int64, productCode string, units []string) ([]Entitlement, error) {
	rows, err := s.pool.Query(ctx, `
SELECT product_code, '' AS product_name, unit, edition, quota_type,
       granted, used, rollover_bal, balance, period_end::text, status
FROM product_entitlements
WHERE tenant_id = $1 AND product_code = $2 AND unit = ANY($3)
ORDER BY unit, edition`, tenantID, productCode, units)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Entitlement
	for rows.Next() {
		var item Entitlement
		if err := rows.Scan(&item.ProductCode, &item.ProductName, &item.Unit, &item.Edition,
			&item.QuotaType, &item.Granted, &item.Used, &item.RolloverBal, &item.Balance,
			&item.PeriodEnd, &item.Status); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}
