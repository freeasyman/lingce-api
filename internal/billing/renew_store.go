package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// selectServicePeriodForUpdateSQL 锁住租户最新一条订阅并读回服务期。
// 续费事务与到期任务都通过这把行锁串行化，避免"刚续费就被到期任务清零"。
const selectServicePeriodForUpdateSQL = `
SELECT id,
       to_char(started_on, 'YYYY-MM-DD'),
       to_char(expired_on, 'YYYY-MM-DD')
FROM tenant_subscriptions
WHERE tenant_id = $1
ORDER BY created_at DESC NULLS LAST, id DESC
LIMIT 1
FOR UPDATE`

// updateServicePeriodForRenewSQL 写回新服务期：续费只改 billing 口径的 started_on/expired_on，
// 同时把订阅状态恢复成 active（到期任务可能已把它标成 expired）。
const updateServicePeriodForRenewSQL = `
UPDATE tenant_subscriptions
SET started_on = $2::date,
    expired_on = $3::date,
    status = 'active',
    updated_at = NOW()
WHERE id = $1
RETURNING to_char(started_on, 'YYYY-MM-DD'), to_char(expired_on, 'YYYY-MM-DD')`

// lockTenantEntitlementsSQL 锁住租户全部额度行；续费要对每一行做结转/清零，
// 锁的顺序与列表查询一致（product, unit, edition, quota_type），避免与扣量事务成环。
const lockTenantEntitlementsSQL = `
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
ORDER BY product_code, unit, edition, quota_type
FOR UPDATE`

// upsertRenewedEntitlementSQL 续费时整行覆盖额度状态（granted/used/rollover/balance 都是新周期的值），
// 租户之前没有的行则新插一条。
const upsertRenewedEntitlementSQL = `
INSERT INTO product_entitlements (tenant_id, product_code, unit, edition, quota_type,
       granted, used, rollover_bal, balance, period_end, status)
VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10::date, $11)
ON CONFLICT (tenant_id, product_code, unit, edition, quota_type)
DO UPDATE SET granted = EXCLUDED.granted,
              used = EXCLUDED.used,
              rollover_bal = EXCLUDED.rollover_bal,
              balance = EXCLUDED.balance,
              period_end = EXCLUDED.period_end,
              status = EXCLUDED.status,
              updated_at = NOW()`

// insertRenewLedgerSQL 写续费流水：grant（新授予 / 上期结转）与 usage（清零）共用一条 SQL。
const insertRenewLedgerSQL = `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, linked_order_id, operator)
VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $9)`

// RenewOrder 在一个 pgx.Tx 里完成续费（activate mode=renew）：
//
//  1. 抢占订单行（activated_at IS NULL），影响 0 行直接返回冲突；
//  2. 锁租户行 + 最新订阅行：没有订阅返回 ErrNoSubscription；
//  3. 锁住租户全部额度行，用 planRenewal 算出新服务期、逐行结转与新授予；
//  4. 写回服务期（延长/重算）→ 覆盖额度行 → 写流水；
//  5. 读回订单并提交。
//
// 结转公式本身是纯函数（见 renew.go），这里只负责"同一事务内取数 → 落库"。
func (s *Store) RenewOrder(ctx context.Context, input RenewOrderInput) (Order, ServicePeriod, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("begin renew order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tenantID int64
	err = tx.QueryRow(ctx, claimOrderForActivationSQL,
		input.OrderID,
		input.ActorID,
		input.AllowedStatuses,
	).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ServicePeriod{}, s.classifyOrderConflict(ctx, tx, input.OrderID)
	}
	if err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("claim order for renewal: %w", err)
	}

	// 与开通/到期任务使用同一把租户行锁，串行化同一租户的服务期变更。
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return Order{}, ServicePeriod{}, err
	}

	var (
		subscriptionID int64
		current        ServicePeriod
	)
	err = tx.QueryRow(ctx, selectServicePeriodForUpdateSQL, tenantID).
		Scan(&subscriptionID, &current.StartedOn, &current.ExpiredOn)
	if errors.Is(err, pgx.ErrNoRows) {
		// 续费必须先有服务期；没有订阅时让运营先走开通（默认 mode=grant）。
		return Order{}, ServicePeriod{}, ErrNoSubscription
	}
	if err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("lock tenant subscription for renewal: %w", err)
	}

	rows, err := lockTenantEntitlements(ctx, tx, tenantID)
	if err != nil {
		return Order{}, ServicePeriod{}, err
	}

	plan, err := planRenewal(current, dateOnly(input.Today), rows, input.Grants, input.LedgerSource, input.Reason)
	if err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("plan renewal: %w", err)
	}
	// started_on 可能是 NULL（历史订阅行），此时保持 NULL；expired_on 必须有值。
	var startedOn any
	if plan.Period.StartedOn != nil {
		startedOn = strings.TrimSpace(*plan.Period.StartedOn)
	}
	expiredOn := serviceDateText(plan.Period.ExpiredOn)
	if expiredOn == "" {
		return Order{}, ServicePeriod{}, errors.New("renewal plan has an empty expired_on")
	}

	var period ServicePeriod
	if err := tx.QueryRow(ctx, updateServicePeriodForRenewSQL, subscriptionID, startedOn, expiredOn).
		Scan(&period.StartedOn, &period.ExpiredOn); err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("extend tenant subscription: %w", err)
	}

	for _, update := range plan.Updates {
		if _, err := tx.Exec(ctx, upsertRenewedEntitlementSQL,
			tenantID,
			update.ProductCode,
			update.Unit,
			update.Edition,
			update.QuotaType,
			update.Granted.String(),
			update.Used.String(),
			update.RolloverBal.String(),
			update.Balance.String(),
			update.PeriodEnd,
			update.Status,
		); err != nil {
			return Order{}, ServicePeriod{}, fmt.Errorf("apply renewed entitlement %s/%s/%s/%s: %w",
				update.ProductCode, update.Unit, update.Edition, update.QuotaType, err)
		}
	}

	for _, entry := range plan.Ledger {
		if _, err := tx.Exec(ctx, insertRenewLedgerSQL,
			tenantID,
			entry.ProductCode,
			entry.Unit,
			entry.Type,
			entry.Amount.String(),
			entry.Source,
			entry.Reason,
			input.OrderID,
			input.Operator,
		); err != nil {
			return Order{}, ServicePeriod{}, fmt.Errorf("insert renewal ledger %s/%s: %w",
				entry.ProductCode, entry.Unit, err)
		}
	}

	order, err := queryOrder(ctx, tx, input.OrderID)
	if err != nil {
		return Order{}, ServicePeriod{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("commit renew order transaction: %w", err)
	}
	return order, period, nil
}

// lockTenantEntitlements 锁住并返回租户全部额度行（续费逐行结转的数据来源）。
func lockTenantEntitlements(ctx context.Context, tx pgx.Tx, tenantID int64) ([]Entitlement, error) {
	rows, err := tx.Query(ctx, lockTenantEntitlementsSQL, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query tenant entitlements for update: %w", err)
	}
	defer rows.Close()

	items := make([]Entitlement, 0, 16)
	for rows.Next() {
		item, err := scanEntitlementRow(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan tenant entitlements for update: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant entitlements for update: %w", err)
	}
	return items, nil
}

// serviceDateText 取出 ServicePeriod 的日期文本；nil 返回空串由调用方拒绝。
func serviceDateText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
