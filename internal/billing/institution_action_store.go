package billing

// 机构动作（开通/加量/赠送/续期/加产品/延长试用）的事务型写入。
//
// 动作统一在一个 pgx.Tx 里完成「锁租户 → 建订单 → 发额度/写流水 → 写服务期 →（可选）收款核销」，
// 任何一步失败整体回滚；同一租户的并发动作由 tenants 行锁串行化。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// institutionOrderInput 是机构动作生成订单的落库入参。
// 金额由 service 算好（套餐价 / 按天折算），items 的价格已按总额分摊。
type institutionOrderInput struct {
	OrderNo          string
	TenantID         int64
	OrderType        string
	Items            []OrderItem
	TotalAmount      Decimal
	PayableAmount    Decimal
	Source           string
	SalesmanID       *int64
	ChannelID        *int64
	PriceListVersion string
	Status           string
	Reason           string
	ApprovedBy       *int64
	PaidAmount       Decimal
	PaidAt           *time.Time
	ConfirmedBy      *int64
	ActivatedBy      *int64
}

// insertInstitutionOrderSQL 写入一笔订单；ActivatedBy 非空时同时标记 activated_at。
const insertInstitutionOrderSQL = `
INSERT INTO billing_orders (order_no, tenant_id, order_type, items, total_amount, payable_amount,
       source, salesman_id, channel_id, price_list_version, status, reason, approved_by,
       paid_amount, paid_at, confirmed_by, activated_at, activated_by)
VALUES ($1, $2, $3, $4::jsonb, $5::numeric, $6::numeric,
        $7, $8, $9, $10, $11, $12, $13,
        $14::numeric, $15, $16, CASE WHEN $17::bigint IS NULL THEN NULL ELSE NOW() END, $17)
RETURNING ` + orderColumns

// InsertInstitutionOrderTx 写订单；order_no 撞号返回 ErrOrderNoExists 由 service 换号重试。
func (s *Store) InsertInstitutionOrderTx(ctx context.Context, tx pgx.Tx, input institutionOrderInput) (Order, error) {
	items := input.Items
	if items == nil {
		items = []OrderItem{}
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return Order{}, fmt.Errorf("encode order items: %w", err)
	}
	order, err := scanOrder(tx.QueryRow(ctx, insertInstitutionOrderSQL,
		input.OrderNo,
		input.TenantID,
		input.OrderType,
		string(encoded),
		input.TotalAmount.String(),
		input.PayableAmount.String(),
		input.Source,
		input.SalesmanID,
		input.ChannelID,
		input.PriceListVersion,
		input.Status,
		input.Reason,
		input.ApprovedBy,
		input.PaidAmount.String(),
		input.PaidAt,
		input.ConfirmedBy,
		input.ActivatedBy,
	).Scan)
	if err != nil {
		if isUniqueViolation(err) {
			return Order{}, ErrOrderNoExists
		}
		return Order{}, fmt.Errorf("insert institution order: %w", err)
	}
	return order, nil
}

// entitlementWriteMode 决定额度写入语义：
//   - set：开通时覆盖（重开/试用转正式按新标准档重置，避免重复发放叠加）；
//   - add：加量/赠送/延长试用时累加。
type entitlementWriteMode int

const (
	entitlementWriteSet entitlementWriteMode = iota
	entitlementWriteAdd
)

// WriteEntitlementTx 写一行额度。
func (s *Store) WriteEntitlementTx(ctx context.Context, tx pgx.Tx, mode entitlementWriteMode,
	tenantID int64, productCode, unit, edition, quotaType string, amount Decimal, periodEnd string) error {
	var sql string
	if mode == entitlementWriteSet {
		sql = `
INSERT INTO product_entitlements (tenant_id, product_code, unit, edition, quota_type, granted, used, rollover_bal, balance, period_end, status)
VALUES ($1, $2, $3, $4, $5, $6::numeric, 0, 0, $6::numeric, $7::date, 'active')
ON CONFLICT (tenant_id, product_code, unit, edition, quota_type)
DO UPDATE SET granted = EXCLUDED.granted,
              used = 0,
              rollover_bal = 0,
              balance = EXCLUDED.granted,
              period_end = EXCLUDED.period_end,
              status = 'active',
              updated_at = NOW()`
	} else {
		sql = `
INSERT INTO product_entitlements (tenant_id, product_code, unit, edition, quota_type, granted, used, rollover_bal, balance, period_end, status)
VALUES ($1, $2, $3, $4, $5, $6::numeric, 0, 0, $6::numeric, $7::date, 'active')
ON CONFLICT (tenant_id, product_code, unit, edition, quota_type)
DO UPDATE SET granted = product_entitlements.granted + EXCLUDED.granted,
              balance = product_entitlements.balance + EXCLUDED.balance,
              period_end = EXCLUDED.period_end,
              status = 'active',
              updated_at = NOW()`
	}
	if _, err := tx.Exec(ctx, sql, tenantID, productCode, unit, edition, quotaType, amount.String(), periodEnd); err != nil {
		return fmt.Errorf("write entitlement %s/%s: %w", productCode, unit, err)
	}
	return nil
}

// InsertGrantLedgerTx 写一条发放流水（grant）。
func (s *Store) InsertGrantLedgerTx(ctx context.Context, tx pgx.Tx, tenantID int64, productCode, unit string,
	amount Decimal, source, reason string, orderID *int64, operator string) error {
	if _, err := tx.Exec(ctx, insertGrantLedgerSQL,
		tenantID, productCode, unit, amount.String(), source, reason, orderID, operator); err != nil {
		return fmt.Errorf("insert grant ledger %s/%s: %w", productCode, unit, err)
	}
	return nil
}

// InsertUsageLedgerTx 写一条扣减流水（usage，amount 为负数；续期清理试用额度用）。
func (s *Store) InsertUsageLedgerTx(ctx context.Context, tx pgx.Tx, tenantID int64, productCode, unit string,
	amount Decimal, source, reason string, operator string) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, operator)
VALUES ($1, $2, $3, 'usage', $4::numeric, $5, $6, $7)`,
		tenantID, productCode, unit, amount.String(), source, reason, operator); err != nil {
		return fmt.Errorf("insert usage ledger %s/%s: %w", productCode, unit, err)
	}
	return nil
}

// LoadEntitlementRowsForUpdateTx 锁住并读出机构全部额度行（续期结转用）。
func (s *Store) LoadEntitlementRowsForUpdateTx(ctx context.Context, tx pgx.Tx, tenantID int64) ([]Entitlement, error) {
	rows, err := tx.Query(ctx, `
SELECT product_code, unit, edition, quota_type,
       granted::text, used::text, rollover_bal::text, balance::text,
       to_char(period_end, 'YYYY-MM-DD'), status
FROM product_entitlements
WHERE tenant_id = $1
ORDER BY product_code, unit, edition, quota_type
FOR UPDATE`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("lock entitlements: %w", err)
	}
	defer rows.Close()

	items := make([]Entitlement, 0, 16)
	for rows.Next() {
		var (
			item                         Entitlement
			granted, used, rollover, bal string
		)
		if err := rows.Scan(&item.ProductCode, &item.Unit, &item.Edition, &item.QuotaType,
			&granted, &used, &rollover, &bal, &item.PeriodEnd, &item.Status); err != nil {
			return nil, fmt.Errorf("scan locked entitlement: %w", err)
		}
		item.Granted = Decimal(granted)
		item.Used = Decimal(used)
		item.RolloverBal = Decimal(rollover)
		item.Balance = Decimal(bal)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locked entitlements: %w", err)
	}
	return items, nil
}

// UpdateEntitlementRowTx 覆盖一行额度的全部字段（续期结转 / 试用清零）。
func (s *Store) UpdateEntitlementRowTx(ctx context.Context, tx pgx.Tx, tenantID int64, row Entitlement,
	granted, used, rollover, balance Decimal, periodEnd, status string) error {
	tag, err := tx.Exec(ctx, `
UPDATE product_entitlements
SET granted = $6::numeric,
    used = $7::numeric,
    rollover_bal = $8::numeric,
    balance = $9::numeric,
    period_end = $10::date,
    status = $11,
    updated_at = NOW()
WHERE tenant_id = $1
  AND product_code = $2
  AND unit = $3
  AND edition = $4
  AND quota_type = $5`,
		tenantID, row.ProductCode, row.Unit, row.Edition, row.QuotaType,
		granted.String(), used.String(), rollover.String(), balance.String(), periodEnd, status)
	if err != nil {
		return fmt.Errorf("update entitlement %s/%s: %w", row.ProductCode, row.Unit, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("update entitlement %s/%s: row disappeared", row.ProductCode, row.Unit)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 服务期写入
// ---------------------------------------------------------------------------

// lockActivePeriodTx 锁住当前 active 服务期；没有返回 (nil, nil)。
// clearTrialEntitlementsTx 把指定产品的试用额度作废（granted/used/rollover/balance 归零，
// 状态 cleared），并写一条 usage/trial_cleared 流水记录作废前的余额。
//
// 用于「试用转正式」：界面与通知函都承诺「试用额度作废、发放全新一年正式额度」，
// 但如果只发正式额度、不关试用额度行，扣量会先吃试用（结转→试用→正式），
// 客户实际拿到的额度多于承诺，且通知函上的数字对不上。
func (s *Store) clearTrialEntitlementsTx(ctx context.Context, tx pgx.Tx, tenantID int64,
	productCodes []string, operator string) error {
	if len(productCodes) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
SELECT product_code, unit, balance
FROM product_entitlements
WHERE tenant_id = $1
  AND quota_type = 'trial'
  AND status = 'active'
  AND product_code = ANY($2)
FOR UPDATE`, tenantID, productCodes)
	if err != nil {
		return err
	}
	type trialRow struct {
		productCode string
		unit        string
		balance     string
	}
	var cleared []trialRow
	for rows.Next() {
		var r trialRow
		if err := rows.Scan(&r.productCode, &r.unit, &r.balance); err != nil {
			rows.Close()
			return err
		}
		cleared = append(cleared, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(cleared) == 0 {
		return nil
	}

	if _, err := tx.Exec(ctx, `
UPDATE product_entitlements
SET granted = 0,
    used = 0,
    rollover_bal = 0,
    balance = 0,
    status = 'cleared',
    updated_at = NOW()
WHERE tenant_id = $1
  AND quota_type = 'trial'
  AND status = 'active'
  AND product_code = ANY($2)`, tenantID, productCodes); err != nil {
		return err
	}

	for _, r := range cleared {
		if _, err := tx.Exec(ctx, `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, operator)
VALUES ($1, $2, $3, 'usage', $4::numeric * -1, 'expired', '试用转正式：试用额度作废', $5)`,
			tenantID, r.productCode, r.unit, r.balance, operator); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LockActivePeriodTx(ctx context.Context, tx pgx.Tx, tenantID int64) (*periodRow, error) {
	item, err := scanPeriod(tx.QueryRow(ctx, periodSelect+`
WHERE tenant_id = $1 AND status = 'active'
ORDER BY started_on DESC, id DESC
LIMIT 1
FOR UPDATE`, tenantID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock active subscription period: %w", err)
	}
	return &item, nil
}

// CloseActivePeriodTx 把当前 active 服务期关成 expired/terminated。
func (s *Store) CloseActivePeriodTx(ctx context.Context, tx pgx.Tx, tenantID int64, status string) error {
	if _, err := tx.Exec(ctx, `
UPDATE subscription_periods
SET status = $2, updated_at = NOW()
WHERE tenant_id = $1 AND status = 'active'`, tenantID, status); err != nil {
		return fmt.Errorf("close active subscription period: %w", err)
	}
	return nil
}

// InsertActivePeriodTx 插入一条 active 服务期。
func (s *Store) InsertActivePeriodTx(ctx context.Context, tx pgx.Tx, tenantID int64, startedOn, expiredOn time.Time, source string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source)
VALUES ($1, $2::date, $3::date, 'active', $4)
RETURNING id`, tenantID, formatDateOnly(startedOn), formatDateOnly(expiredOn), source).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrPeerConflict
		}
		return 0, fmt.Errorf("insert subscription period: %w", err)
	}
	return id, nil
}

// ExtendActivePeriodTx 顺延当前 active 服务期的到期日；没有 active 时返回 false。
func (s *Store) ExtendActivePeriodTx(ctx context.Context, tx pgx.Tx, tenantID int64, expiredOn time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `
UPDATE subscription_periods
SET expired_on = $2::date, updated_at = NOW()
WHERE tenant_id = $1 AND status = 'active'`, tenantID, formatDateOnly(expiredOn))
	if err != nil {
		return false, fmt.Errorf("extend active subscription period: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// UpdatePeriodRowTx 覆盖一条服务期（手工调整状态/日期）。
func (s *Store) UpdatePeriodRowTx(ctx context.Context, tx pgx.Tx, tenantID, periodID int64, startedOn, expiredOn time.Time, status, source string) error {
	tag, err := tx.Exec(ctx, `
UPDATE subscription_periods
SET started_on = $3::date,
    expired_on = $4::date,
    status = $5,
    source = $6,
    updated_at = NOW()
WHERE tenant_id = $1 AND id = $2`, tenantID, periodID, formatDateOnly(startedOn), formatDateOnly(expiredOn), status, source)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrPeerConflict
		}
		return fmt.Errorf("update subscription period: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPeerNotFound
	}
	return nil
}

// InsertManualPeriodTx 手工插入一条服务期（状态可为 active/pending/expired/terminated）。
func (s *Store) InsertManualPeriodTx(ctx context.Context, tx pgx.Tx, tenantID int64, startedOn, expiredOn time.Time, status, source string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO subscription_periods (tenant_id, started_on, expired_on, status, source)
VALUES ($1, $2::date, $3::date, $4, $5)
RETURNING id`, tenantID, formatDateOnly(startedOn), formatDateOnly(expiredOn), status, source).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrPeerConflict
		}
		return 0, fmt.Errorf("insert subscription period: %w", err)
	}
	return id, nil
}

// EnsureSubscriptionPlanTx 取套餐 id；不存在时补一条最小套餐行（标准版/试用版）。
func (s *Store) EnsureSubscriptionPlanTx(ctx context.Context, tx pgx.Tx, code, name string, durationDays int) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
SELECT id FROM tenant_subscription_plans WHERE lower(COALESCE(code, '')) = lower($1) ORDER BY id LIMIT 1`, code).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("query subscription plan: %w", err)
	}
	if durationDays <= 0 {
		durationDays = 365
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tenant_subscription_plans (code, name, duration_days)
VALUES ($1, $2, $3)
ON CONFLICT (code) DO NOTHING`, code, name, durationDays); err != nil {
		return 0, fmt.Errorf("insert subscription plan: %w", err)
	}
	if err := tx.QueryRow(ctx, `
SELECT id FROM tenant_subscription_plans WHERE lower(COALESCE(code, '')) = lower($1) ORDER BY id LIMIT 1`, code).Scan(&id); err != nil {
		return 0, fmt.Errorf("query subscription plan after insert: %w", err)
	}
	return id, nil
}

// UpsertTenantSubscriptionTx 同步 tenant_subscriptions（存量客户侧逻辑仍读这张表）。
func (s *Store) UpsertTenantSubscriptionTx(ctx context.Context, tx pgx.Tx, tenantID, planID int64, startedOn, expiredOn time.Time) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO tenant_subscriptions (tenant_id, plan_id, status, started_on, expired_on, updated_at)
VALUES ($1, $2, 'active', $3::date, $4::date, NOW())
ON CONFLICT (tenant_id) DO UPDATE
SET plan_id = EXCLUDED.plan_id,
    status = 'active',
    started_on = EXCLUDED.started_on,
    expired_on = EXCLUDED.expired_on,
    updated_at = NOW()`, tenantID, planID, formatDateOnly(startedOn), formatDateOnly(expiredOn)); err != nil {
		return fmt.Errorf("upsert tenant subscription: %w", err)
	}
	return nil
}

// SyncTenantValidityTx 把服务期同步到 tenants 的有效期列：
// 客户侧登录校验读 COALESCE(valid_to, service_expired_on)，不同步会出现
// 「服务期已续费但账号仍被判定过期」的问题。
func (s *Store) SyncTenantValidityTx(ctx context.Context, tx pgx.Tx, tenantID int64, accountMode string, startedOn, expiredOn time.Time) error {
	if _, err := tx.Exec(ctx, `
UPDATE tenants
SET account_mode = $2,
    valid_from = $3::date::timestamp,
    valid_to = $4::date::timestamp,
    service_started_on = $3::date,
    service_expired_on = $4::date,
    updated_at = NOW()
WHERE id = $1`, tenantID, accountMode, formatDateOnly(startedOn), formatDateOnly(expiredOn)); err != nil {
		return fmt.Errorf("sync tenant validity: %w", err)
	}
	return nil
}
