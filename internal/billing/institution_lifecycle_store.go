package billing

// 服务生命周期与更正动作的数据访问层：
//   · 暂停/恢复：只写 tenants.paused_at / paused_reason；
//   · 取消：一个事务里写 cancelled_at、终止当前服务期、同步 tenant_subscriptions、
//     清零额度并按清零前余额写 expired 流水；
//   · 作废订单：billing_orders.status='void'；
//   · 调额度：锁额度行后改 granted/balance 并写 correction 流水。
//
// 所有写方法都要求调用方已开启事务；service 层负责权限校验、入参校验与审计日志。

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PauseTenantTx 写入暂停标记；已取消的机构返回 ErrInstitutionCancelled。
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
  AND cancelled_at IS NULL
  AND paused_at IS NULL`, tenantID, reason)
	if err != nil {
		return false, fmt.Errorf("pause tenant: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	// 区分「已经暂停（幂等）」与「已取消（冲突）」。
	var paused, cancelled bool
	if err := tx.QueryRow(ctx, `
SELECT paused_at IS NOT NULL, cancelled_at IS NOT NULL
FROM tenants WHERE id = $1`, tenantID).Scan(&paused, &cancelled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrInstitutionNotFound
		}
		return false, fmt.Errorf("classify pause conflict: %w", err)
	}
	if cancelled {
		return false, ErrInstitutionCancelled
	}
	// paused 或行锁竞争导致没写：都算幂等成功（不重复写）。
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
  AND cancelled_at IS NULL
  AND paused_at IS NOT NULL`, tenantID)
	if err != nil {
		return false, fmt.Errorf("resume tenant: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return true, nil
	}
	var cancelled bool
	if err := tx.QueryRow(ctx, `SELECT cancelled_at IS NOT NULL FROM tenants WHERE id = $1`, tenantID).Scan(&cancelled); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrInstitutionNotFound
		}
		return false, fmt.Errorf("classify resume conflict: %w", err)
	}
	if cancelled {
		return false, ErrInstitutionCancelled
	}
	return false, nil
}

// MarkTenantCancelledTx 写入取消标记；已经取消返回 RowsAffected=0（service 幂等返回）。
func (s *Store) MarkTenantCancelledTx(ctx context.Context, tx pgx.Tx, tenantID int64, reason string) (bool, error) {
	tag, err := tx.Exec(ctx, `
UPDATE tenants
SET cancelled_at = NOW(),
    cancelled_reason = $2,
    paused_at = NULL,
    paused_reason = '',
    updated_at = NOW()
WHERE id = $1
  AND cancelled_at IS NULL`, tenantID, reason)
	if err != nil {
		return false, fmt.Errorf("cancel tenant: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// TerminatePeriodTx 把指定的当前服务期置 terminated（不按 status 过滤：
// 已到期但尚未被任务关单的服务期也要结束，取消后不能再被到期任务复活）。
func (s *Store) TerminatePeriodTx(ctx context.Context, tx pgx.Tx, tenantID, periodID int64) error {
	if periodID <= 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE subscription_periods
SET status = 'terminated', updated_at = NOW()
WHERE tenant_id = $1 AND id = $2`, tenantID, periodID); err != nil {
		return fmt.Errorf("terminate subscription period: %w", err)
	}
	return nil
}

// TerminateTenantSubscriptionTx 同步存量 tenant_subscriptions（客户侧登录仍读这张表）。
func (s *Store) TerminateTenantSubscriptionTx(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	if _, err := tx.Exec(ctx, `
UPDATE tenant_subscriptions
SET status = 'terminated',
    paused_at = NULL,
    updated_at = NOW()
WHERE tenant_id = $1`, tenantID); err != nil {
		return fmt.Errorf("terminate tenant subscription: %w", err)
	}
	return nil
}

// ClearEntitlementsTx 把机构全部额度行归零并置 cleared，返回被清零的行数。
// 清零前的余额由 service 先读出来写 usage/expired 流水，保证账实一致。
func (s *Store) ClearEntitlementsTx(ctx context.Context, tx pgx.Tx, tenantID int64) (int64, error) {
	tag, err := tx.Exec(ctx, `
UPDATE product_entitlements
SET granted = 0,
    used = 0,
    rollover_bal = 0,
    balance = 0,
    status = 'cleared',
    updated_at = NOW()
WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return 0, fmt.Errorf("clear entitlements: %w", err)
	}
	return tag.RowsAffected(), nil
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
	amountCents int64, reason, operator string) error {
	ledgerType := LedgerTypeGrant
	amount := centsToDecimal(amountCents)
	if amountCents < 0 {
		ledgerType = LedgerTypeUsage
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, operator)
VALUES ($1, $2, $3, $4, $5::numeric, 'correction', $6, $7)`,
		tenantID, productCode, unit, ledgerType, amount.String(), reason, operator); err != nil {
		return fmt.Errorf("insert correction ledger: %w", err)
	}
	return nil
}
