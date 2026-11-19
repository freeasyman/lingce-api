package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// listExpiringTenantIDsSQL 选出"服务期已过且仍是 active"的租户。
// expired_on 为 NULL 的历史行不参与（NULL < date 不成立），避免误清没有到期日的订阅。
const listExpiringTenantIDsSQL = `
SELECT DISTINCT tenant_id
FROM subscription_periods
WHERE expired_on < $1::date
  AND status = 'active'
ORDER BY tenant_id`

// ListExpiringTenantIDs 返回到期（expired_on < today 且 status='active'）的租户 id。
func (s *Store) ListExpiringTenantIDs(ctx context.Context, today string) ([]int64, error) {
	rows, err := s.pool.Query(ctx, listExpiringTenantIDsSQL, today)
	if err != nil {
		return nil, fmt.Errorf("query expiring tenants: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0, 16)
	for rows.Next() {
		var tenantID int64
		if err := rows.Scan(&tenantID); err != nil {
			return nil, fmt.Errorf("scan expiring tenant: %w", err)
		}
		ids = append(ids, tenantID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expiring tenants: %w", err)
	}
	return ids, nil
}

// expireTenantSubscriptionsSQL 把到期租户的订阅置为 expired。
// 带 expired_on < today 条件是为了并发安全：如果另一个事务刚刚续费延期，
// 这条 UPDATE 会命中 0 行，调用方据此跳过清零。
const expireTenantSubscriptionsSQL = `
UPDATE tenant_subscriptions
SET status = 'expired',
    updated_at = NOW()
WHERE tenant_id = $1
  AND status = 'active'
  AND expired_on < $2::date`

// clearTenantEntitlementsSQL 清空租户全部额度行（granted/used/rollover/balance 归零，状态 cleared）。
const clearTenantEntitlementsSQL = `
UPDATE product_entitlements
SET granted = 0,
    used = 0,
    rollover_bal = 0,
    balance = 0,
    status = 'cleared',
    updated_at = NOW()
WHERE tenant_id = $1`

// insertExpiredLedgerSQL 写到期清零流水（usage/expired，不关联订单）。
const insertExpiredLedgerSQL = `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, operator)
VALUES ($1, $2, $3, 'usage', $4::numeric, 'expired', $5, $6)`

// ExpireTenant 在一个事务里清空一个到期租户，返回被清理的额度行数：
//
//  1. 锁租户行（与开通/续费同一把锁），锁不到说明租户已删除，直接跳过；
//  2. tenant_subscriptions.status = 'expired'（只更新已过期的 active 行）；
//  3. 锁住并读出该租户全部额度行，按行生成 usage/expired 流水（amount = -旧余额）；
//  4. 全部额度行清零并把状态置为 cleared；
//  5. 提交。
//
// 若第 2 步命中 0 行（并发续费已经延期），整个事务不做任何修改并返回 0。
func (s *Store) ExpireTenant(ctx context.Context, tenantID int64, today string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expire tenant transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 租户已被删除，残留订阅不必清额度（没有租户可访问这些额度）。
			return 0, nil
		}
		return 0, err
	}

	// 到期口径：合同行。把 active 合同行关成 expired（并发续费延期时命中 0 行，跳过清零）。
	// 两套分开后不再写 tenant_subscriptions（老产品数据）。
	tag, err := tx.Exec(ctx, `
UPDATE subscription_periods
SET status = 'expired', updated_at = NOW()
WHERE tenant_id = $1
  AND status = 'active'
  AND expired_on < $2::date`, tenantID, today)
	if err != nil {
		return 0, fmt.Errorf("mark subscription period expired: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}

	rows, err := lockTenantEntitlements(ctx, tx, tenantID)
	if err != nil {
		return 0, err
	}
	entries, err := planExpiryLedger(rows)
	if err != nil {
		return 0, err
	}

	if _, err := tx.Exec(ctx, clearTenantEntitlementsSQL, tenantID); err != nil {
		return 0, fmt.Errorf("clear tenant entitlements: %w", err)
	}
	for _, entry := range entries {
		if _, err := tx.Exec(ctx, insertExpiredLedgerSQL,
			tenantID,
			entry.ProductCode,
			entry.Unit,
			entry.Amount.String(),
			reasonSubscriptionExpired,
			expireOperator,
		); err != nil {
			return 0, fmt.Errorf("insert expired ledger %s/%s: %w", entry.ProductCode, entry.Unit, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expire tenant transaction: %w", err)
	}
	return len(rows), nil
}
