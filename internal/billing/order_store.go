package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// orderColumns 是订单查询/回写的公共列。金额转 text 由 Decimal 承载，items 转 text 交给 OrderItem 解码。
const orderColumns = `id, order_no, tenant_id, order_type, COALESCE(items, '[]'::jsonb)::text,
       total_amount::text, payable_amount::text, source, salesman_id, channel_id, price_list_version,
       paid_amount::text, paid_at, confirmed_by, status, reason, approved_by,
       activated_at, activated_by, created_at, updated_at`

const insertOrderSQL = `
INSERT INTO billing_orders (order_no, tenant_id, order_type, items, total_amount, payable_amount,
       source, salesman_id, channel_id, price_list_version, status, reason, approved_by)
VALUES ($1, $2, $3, $4::jsonb, $5::numeric, $6::numeric, $7, $8, $9, $10, 'created', $11, $12)
RETURNING ` + orderColumns

// InsertOrder 写入一笔 created 订单；order_no 唯一索引冲突返回 ErrOrderNoExists 由 service 换号重试。
func (s *Store) InsertOrder(ctx context.Context, input CreateOrderInput) (Order, error) {
	items := input.Items
	if items == nil {
		items = []OrderItem{}
	}
	encodedItems, err := json.Marshal(items)
	if err != nil {
		return Order{}, fmt.Errorf("encode order items: %w", err)
	}
	order, err := scanOrder(s.pool.QueryRow(ctx, insertOrderSQL,
		input.OrderNo,
		input.TenantID,
		input.OrderType,
		string(encodedItems),
		input.TotalAmount.String(),
		input.PayableAmount.String(),
		input.Source,
		input.SalesmanID,
		input.ChannelID,
		input.PriceListVersion,
		input.Reason,
		input.ApprovedBy,
	).Scan)
	if err != nil {
		if isUniqueViolation(err) {
			return Order{}, ErrOrderNoExists
		}
		return Order{}, fmt.Errorf("insert order: %w", err)
	}
	return order, nil
}

// ListOrders 按租户/状态/类型过滤并按创建时间倒序分页，返回当页数据与总条数。
func (s *Store) ListOrders(ctx context.Context, query OrderQuery) ([]Order, int64, error) {
	query = NormalizeOrderQuery(query)

	conditions := []string{"1 = 1"}
	args := make([]any, 0, 3)
	if query.TenantID > 0 {
		args = append(args, query.TenantID)
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if query.Status != "" {
		args = append(args, query.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}
	if query.OrderType != "" {
		args = append(args, query.OrderType)
		conditions = append(conditions, fmt.Sprintf("order_type = $%d", len(args)))
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM billing_orders WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count billing orders: %w", err)
	}

	pageArgs := append(append(make([]any, 0, len(args)+2), args...), query.Offset(), int64(query.PageSize))
	pageSQL := fmt.Sprintf(`SELECT %s
FROM billing_orders
WHERE %s
ORDER BY created_at DESC, id DESC
LIMIT $%d OFFSET $%d`, orderColumns, where, len(args)+1, len(args)+2)

	rows, err := s.pool.Query(ctx, pageSQL, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("query billing orders: %w", err)
	}
	defer rows.Close()

	items := make([]Order, 0, query.PageSize)
	for rows.Next() {
		order, err := scanOrder(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan billing order: %w", err)
		}
		items = append(items, order)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate billing orders: %w", err)
	}
	return items, total, nil
}

// orderQuerier 让订单查询同时适配连接池与事务（开通事务内读回订单）。
type orderQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GetOrder 按 id 查询订单，不存在返回 ErrOrderNotFound。
func (s *Store) GetOrder(ctx context.Context, orderID int64) (Order, error) {
	return queryOrder(ctx, s.pool, orderID)
}

// queryOrder 是 GetOrder 与开通事务共用的读回逻辑。
func queryOrder(ctx context.Context, querier orderQuerier, orderID int64) (Order, error) {
	order, err := scanOrder(querier.QueryRow(ctx,
		`SELECT `+orderColumns+` FROM billing_orders WHERE id = $1`, orderID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("query order: %w", err)
	}
	return order, nil
}

const settleOrderSQL = `
UPDATE billing_orders
SET paid_amount = $2::numeric,
    paid_at = $3,
    confirmed_by = $4,
    status = 'settled',
    updated_at = NOW()
WHERE id = $1
  AND status = 'created'
RETURNING ` + orderColumns

// SettleOrder 做回款确认：只有 created 状态能被改成 settled。
// 金额是否等于应付由 service 校验；这里再以 status='created' 做并发兜底。
func (s *Store) SettleOrder(ctx context.Context, input SettleOrderInput) (Order, error) {
	order, err := scanOrder(s.pool.QueryRow(ctx, settleOrderSQL,
		input.OrderID,
		input.PaidAmount.String(),
		input.PaidAt,
		input.ConfirmedBy,
	).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, s.classifyOrderConflict(ctx, s.pool, input.OrderID)
	}
	if err != nil {
		return Order{}, fmt.Errorf("settle order: %w", err)
	}
	return order, nil
}

const claimOrderForActivationSQL = `
UPDATE billing_orders
SET activated_at = NOW(),
    activated_by = $2,
    updated_at = NOW()
WHERE id = $1
  AND activated_at IS NULL
  AND status = ANY($3::text[])
RETURNING tenant_id`

const selectServicePeriodSQL = `
SELECT to_char(s.started_on, 'YYYY-MM-DD'),
       to_char(s.expired_on, 'YYYY-MM-DD')
FROM tenant_subscriptions s
WHERE s.tenant_id = $1
ORDER BY s.created_at DESC NULLS LAST, s.id DESC
LIMIT 1`

const selectStandardPlanSQL = `
SELECT id
FROM tenant_subscription_plans
WHERE lower(COALESCE(code, '')) = 'standard'
ORDER BY id
LIMIT 1`

const insertServicePeriodSQL = `
INSERT INTO tenant_subscriptions (tenant_id, plan_id, status, started_on, expired_on)
VALUES ($1, $2, 'active', CURRENT_DATE, (CURRENT_DATE + INTERVAL '12 months' - INTERVAL '1 day')::date)
RETURNING to_char(started_on, 'YYYY-MM-DD'), to_char(expired_on, 'YYYY-MM-DD')`

const upsertGrantEntitlementSQL = `
INSERT INTO product_entitlements (tenant_id, product_code, unit, edition, quota_type, granted, balance, period_end, status)
VALUES ($1, $2, $3, $4, $5, $6::numeric, $6::numeric, $7::date, 'active')
ON CONFLICT (tenant_id, product_code, unit, edition, quota_type)
DO UPDATE SET granted = product_entitlements.granted + EXCLUDED.granted,
              balance = product_entitlements.balance + EXCLUDED.granted,
              period_end = EXCLUDED.period_end,
              status = 'active',
              updated_at = NOW()`

const insertGrantLedgerSQL = `
INSERT INTO entitlement_ledger (tenant_id, product_code, unit, type, amount, source, reason, linked_order_id, operator)
VALUES ($1, $2, $3, 'grant', $4::numeric, $5, $6, $7, $8)`

// ActivateOrder 在一个 pgx.Tx 里完成开通：
//
//  1. 先抢占订单行（activated_at IS NULL 才能成功），影响 0 行直接返回冲突；
//  2. 锁租户行，建立或读回统一服务期（已有订阅不延长 expired_on）；
//  3. 按 items 累加 product_entitlements 并写 entitlement_ledger（grant）；
//  4. 读回订单，提交。
//
// 任何一步失败整体回滚，且因为第 1 步先于发额度，重复点击不会双倍发额度。
func (s *Store) ActivateOrder(ctx context.Context, input ActivateOrderInput) (Order, ServicePeriod, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("begin activate order transaction: %w", err)
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
		return Order{}, ServicePeriod{}, fmt.Errorf("claim order for activation: %w", err)
	}

	// 锁租户行，串行化同一租户的首次开通，避免两个订单并发时各插一条服务期。
	var lockedTenantID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM tenants WHERE id = $1 FOR UPDATE`, tenantID).Scan(&lockedTenantID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Order{}, ServicePeriod{}, fmt.Errorf("tenant %d not found", tenantID)
		}
		return Order{}, ServicePeriod{}, fmt.Errorf("lock tenant for activation: %w", err)
	}

	period, err := ensureServicePeriod(ctx, tx, tenantID)
	if err != nil {
		return Order{}, ServicePeriod{}, err
	}
	periodEnd := *period.ExpiredOn

	for _, grant := range input.Grants {
		if _, err := tx.Exec(ctx, upsertGrantEntitlementSQL,
			tenantID,
			grant.ProductCode,
			grant.Unit,
			grant.Edition,
			grant.QuotaType,
			grant.Amount.String(),
			periodEnd,
		); err != nil {
			return Order{}, ServicePeriod{}, fmt.Errorf("grant entitlement %s/%s: %w", grant.ProductCode, grant.Unit, err)
		}
		if _, err := tx.Exec(ctx, insertGrantLedgerSQL,
			tenantID,
			grant.ProductCode,
			grant.Unit,
			grant.Amount.String(),
			input.LedgerSource,
			input.Reason,
			input.OrderID,
			input.Operator,
		); err != nil {
			return Order{}, ServicePeriod{}, fmt.Errorf("insert grant ledger %s/%s: %w", grant.ProductCode, grant.Unit, err)
		}
	}

	order, err := queryOrder(ctx, tx, input.OrderID)
	if err != nil {
		return Order{}, ServicePeriod{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, ServicePeriod{}, fmt.Errorf("commit activate order transaction: %w", err)
	}
	return order, period, nil
}

// ensureServicePeriod 读回租户统一服务期；没有订阅时按"今天起 12 个月 - 1 天"新建一条。
// 已有订阅不延长 expired_on，保证同租户所有产品共用同一个到期日。
func ensureServicePeriod(ctx context.Context, tx pgx.Tx, tenantID int64) (ServicePeriod, error) {
	var period ServicePeriod
	err := tx.QueryRow(ctx, selectServicePeriodSQL, tenantID).Scan(&period.StartedOn, &period.ExpiredOn)
	if err == nil {
		if period.ExpiredOn == nil || strings.TrimSpace(*period.ExpiredOn) == "" {
			return ServicePeriod{}, fmt.Errorf("tenant %d subscription has no expired_on", tenantID)
		}
		return period, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ServicePeriod{}, fmt.Errorf("query tenant service period: %w", err)
	}

	var planID int64
	if err := tx.QueryRow(ctx, selectStandardPlanSQL).Scan(&planID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ServicePeriod{}, ErrStandardPlanMissing
		}
		return ServicePeriod{}, fmt.Errorf("query standard subscription plan: %w", err)
	}
	if err := tx.QueryRow(ctx, insertServicePeriodSQL, tenantID, planID).Scan(&period.StartedOn, &period.ExpiredOn); err != nil {
		return ServicePeriod{}, fmt.Errorf("create tenant service period: %w", err)
	}
	if period.ExpiredOn == nil || strings.TrimSpace(*period.ExpiredOn) == "" {
		return ServicePeriod{}, fmt.Errorf("tenant %d new subscription has no expired_on", tenantID)
	}
	return period, nil
}

const refundOrderSQL = `
UPDATE billing_orders
SET status = 'refunded',
    updated_at = NOW()
WHERE id = $1
  AND status = 'settled'
  AND activated_at IS NULL
RETURNING ` + orderColumns

// RefundOrder 做开通前全额退款：只有"已回款且未开通"的订单能退。
func (s *Store) RefundOrder(ctx context.Context, input RefundOrderInput) (Order, error) {
	order, err := scanOrder(s.pool.QueryRow(ctx, refundOrderSQL, input.OrderID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, s.classifyOrderConflict(ctx, s.pool, input.OrderID)
	}
	if err != nil {
		return Order{}, fmt.Errorf("refund order: %w", err)
	}
	return order, nil
}

const adminDisplayNameSQL = `
SELECT COALESCE(NULLIF(real_name, ''), username, '')
FROM operations_admins
WHERE id = $1`

// GetAdminDisplayName 取操作人展示名（姓名优先，其次账号），用于写额度流水的 operator。
func (s *Store) GetAdminDisplayName(ctx context.Context, adminID int64) (string, error) {
	var name string
	if err := s.pool.QueryRow(ctx, adminDisplayNameSQL, adminID).Scan(&name); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("query admin display name: %w", err)
	}
	return name, nil
}

// classifyOrderConflict 在 UPDATE 影响 0 行后回读订单，区分"不存在 / 已开通 / 状态非法"。
func (s *Store) classifyOrderConflict(ctx context.Context, querier orderQuerier, orderID int64) error {
	var (
		status      string
		activatedAt *string
	)
	if err := querier.QueryRow(ctx,
		`SELECT status, activated_at::text FROM billing_orders WHERE id = $1`, orderID,
	).Scan(&status, &activatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrderNotFound
		}
		return fmt.Errorf("classify order conflict: %w", err)
	}
	if activatedAt != nil && strings.TrimSpace(*activatedAt) != "" {
		return ErrOrderAlreadyActivated
	}
	return ErrOrderStatusInvalid
}

// scanOrder 统一解析订单一行（列表、详情与事务内读回共用）。
func scanOrder(scan func(dest ...any) error) (Order, error) {
	var (
		order   Order
		items   string
		total   string
		payable string
		paid    string
	)
	if err := scan(
		&order.ID,
		&order.OrderNo,
		&order.TenantID,
		&order.OrderType,
		&items,
		&total,
		&payable,
		&order.Source,
		&order.SalesmanID,
		&order.ChannelID,
		&order.PriceListVersion,
		&paid,
		&order.PaidAt,
		&order.ConfirmedBy,
		&order.Status,
		&order.Reason,
		&order.ApprovedBy,
		&order.ActivatedAt,
		&order.ActivatedBy,
		&order.CreatedAt,
		&order.UpdatedAt,
	); err != nil {
		return Order{}, err
	}
	order.TotalAmount = Decimal(total)
	order.PayableAmount = Decimal(payable)
	order.PaidAmount = Decimal(paid)
	order.Items = []OrderItem{}
	if trimmed := strings.TrimSpace(items); trimmed != "" && trimmed != "[]" {
		if err := json.Unmarshal([]byte(trimmed), &order.Items); err != nil {
			return Order{}, fmt.Errorf("decode order items: %w", err)
		}
		if order.Items == nil {
			order.Items = []OrderItem{}
		}
	}
	return order, nil
}
