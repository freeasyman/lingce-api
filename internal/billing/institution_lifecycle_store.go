package billing

// 服务生命周期与更正动作的数据访问层：
//   · 暂停/恢复：只写 tenants.paused_at / paused_reason；
//   · 作废订单：billing_orders.status='void'；
//   · 调额度：锁额度行后改 granted/balance 并写 correction 流水。
//
// 所有写方法都要求调用方已开启事务；service 层负责权限校验、入参校验与审计日志。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// PauseTenantTx 写入暂停标记。
//
// 幂等由 service 层先判 paused_at 保证；这里再用条件更新兜住并发：
// 两个并发暂停请求只有一个能写成功，另一个 RowsAffected=0（调用方按已暂停处理即可）。
func (s *Store) PauseTenantTx(ctx context.Context, tx pgx.Tx, tenantID int64, reason string) (bool, error) {
	tag, err := tx.Exec(ctx, `
UPDATE tenants
SET paused_at = NOW(),
    paused_reason = $2,
    updated_at = NOW()
WHERE id = $1
  AND paused_at IS NULL`, tenantID, reason)
	if err != nil {
		return false, fmt.Errorf("pause tenant: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	// 已经暂停或行锁竞争导致没写：都按幂等成功处理（不重复写）。
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM tenants WHERE id = $1`, tenantID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrInstitutionNotFound
		}
		return false, fmt.Errorf("classify pause conflict: %w", err)
	}
	return false, nil
}

// ResumeTenantTx 清除暂停标记；未暂停时 RowsAffected=0，由 service 按幂等处理。
func (s *Store) ResumeTenantTx(ctx context.Context, tx pgx.Tx, tenantID int64) (bool, error) {
	tag, err := tx.Exec(ctx, `
UPDATE tenants
SET paused_at = NULL,
    paused_reason = '',
    updated_at = NOW()
WHERE id = $1
  AND paused_at IS NOT NULL`, tenantID)
	if err != nil {
		return false, fmt.Errorf("resume tenant: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListVoidableOrders 列出可作废订单：未收款、未核销且状态为 created。
func (s *Store) ListVoidableOrders(ctx context.Context, tenantID int64, limit int) ([]Order, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
SELECT `+orderColumns+`
FROM billing_orders
WHERE tenant_id = $1
  AND status = 'created'
  AND paid_amount = 0
  AND activated_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list voidable orders: %w", err)
	}
	defer rows.Close()

	items := make([]Order, 0, 8)
	for rows.Next() {
		order, err := scanOrder(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan voidable order: %w", err)
		}
		items = append(items, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate voidable orders: %w", err)
	}
	return items, nil
}

// VoidOrderTx 把订单置 void；不满足「未收款且未核销」时返回 ErrOrderNotVoidable。
// 调用前 service 已锁行并校验状态，这里再用条件更新兜住并发核销。
func (s *Store) VoidOrderTx(ctx context.Context, tx pgx.Tx, orderID int64) (Order, error) {
	order, err := scanOrder(tx.QueryRow(ctx, `
UPDATE billing_orders
SET status = 'void',
    updated_at = NOW()
WHERE id = $1
  AND status = 'created'
  AND paid_amount = 0
  AND activated_at IS NULL
RETURNING `+orderColumns, orderID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotVoidable
	}
	if err != nil {
		return Order{}, fmt.Errorf("void order: %w", err)
	}
	return order, nil
}

// UpdateEntitlementCorrectionTx 按调整量改一行额度：granted 与 balance 同步加减，used 不动。
// 返回更新后的 granted / balance，便于 service 记录日志。
func (s *Store) UpdateEntitlementCorrectionTx(ctx context.Context, tx pgx.Tx, tenantID int64, row Entitlement,
	amountCents int64) (Decimal, Decimal, error) {
	tag, err := tx.Exec(ctx, `
UPDATE product_entitlements
SET granted = granted + $6::numeric,
    balance = balance + $6::numeric,
    updated_at = NOW()
WHERE tenant_id = $1
  AND product_code = $2
  AND unit = $3
  AND edition = $4
  AND quota_type = $5
  AND granted + $6::numeric >= 0
  AND balance + $6::numeric >= 0`,
		tenantID, row.ProductCode, row.Unit, row.Edition, row.QuotaType, centsToDecimal(amountCents))
	if err != nil {
		return "", "", fmt.Errorf("update entitlement correction: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", "", invalidf("调整后余额或已发额度会变成负数，请确认可调余额")
	}
	var granted, balance string
	if err := tx.QueryRow(ctx, `
SELECT granted::text, balance::text
FROM product_entitlements
WHERE tenant_id = $1
  AND product_code = $2
  AND unit = $3
  AND edition = $4
  AND quota_type = $5`,
		tenantID, row.ProductCode, row.Unit, row.Edition, row.QuotaType).Scan(&granted, &balance); err != nil {
		return "", "", fmt.Errorf("read corrected entitlement: %w", err)
	}
	return Decimal(granted), Decimal(balance), nil
}

// InsertCorrectionLedgerTx 写一条 source=correction 的流水：
// 正向记 grant，负向记 usage（amount 带符号，与扣量流水的负数口径一致）。
func (s *Store) InsertCorrectionLedgerTx(ctx context.Context, tx pgx.Tx, tenantID int64, productCode, unit string,
	amountCents int64, reason, operator, idempotencyKey string) error {
	ledgerType := LedgerTypeGrant
	amount := centsToDecimal(amountCents)
	if amountCents < 0 {
		ledgerType = LedgerTypeUsage
	}
	var key any
	if keyText := strings.TrimSpace(idempotencyKey); keyText != "" {
		key = keyText
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, operator, idempotency_key)
VALUES ($1, $2, $3, $4, $5::numeric, 'correction', $6, $7, $8)`,
		tenantID, productCode, unit, ledgerType, amount.String(), reason, operator, key); err != nil {
		return fmt.Errorf("insert correction ledger: %w", err)
	}
	return nil
}

// HasCorrectionLedgerTx 判断同一租户的幂等键是否已用于一笔调额度流水。
func (s *Store) HasCorrectionLedgerTx(ctx context.Context, tx pgx.Tx, tenantID int64, idempotencyKey string) (bool, error) {
	if tenantID <= 0 || strings.TrimSpace(idempotencyKey) == "" {
		return false, nil
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM entitlement_ledger
    WHERE tenant_id = $1 AND idempotency_key = $2
)`, tenantID, strings.TrimSpace(idempotencyKey)).Scan(&exists); err != nil {
		return false, fmt.Errorf("check correction idempotency: %w", err)
	}
	return exists, nil
}
