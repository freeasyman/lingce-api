package billing

// 收款单与核销的数据访问层。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// paymentColumns 是收款单的公共列，金额转 text 交给 Decimal。
const paymentColumns = `id, payment_no, payer_name, amount::text, to_char(received_on, 'YYYY-MM-DD'),
       method, voucher_no, status, note, created_at`

// scanPayment 解析一行收款单。
func scanPayment(scan func(dest ...any) error) (PaymentRecord, error) {
	var (
		item       PaymentRecord
		amount     string
		receivedOn string
		createdAt  time.Time
	)
	if err := scan(&item.ID, &item.No, &item.Payer, &amount, &receivedOn,
		&item.Method, &item.VoucherNo, &item.Status, &item.Note, &createdAt); err != nil {
		return PaymentRecord{}, err
	}
	item.Amount = Decimal(amount)
	item.Date = receivedOn
	return item, nil
}

// InsertPaymentTx 创建一张未核销的收款单。
func (s *Store) InsertPaymentTx(ctx context.Context, tx pgx.Tx, input createPaymentInput) (PaymentRecord, error) {
	payment, err := scanPayment(tx.QueryRow(ctx, `
INSERT INTO billing_payments (payment_no, payer_name, amount, received_on, method, voucher_no, status, note, created_by)
VALUES ($1, $2, $3::numeric, $4::date, $5, $6, 'unallocated', $7, $8)
RETURNING `+paymentColumns,
		input.PaymentNo, input.Payer, input.Amount.String(), input.ReceivedOn,
		input.Method, input.VoucherNo, input.Note, nullableID(input.CreatedBy)).Scan)
	if err != nil {
		if isUniqueViolation(err) {
			return PaymentRecord{}, ErrPeerConflict
		}
		return PaymentRecord{}, fmt.Errorf("insert payment: %w", err)
	}
	return payment, nil
}

// LockPaymentTx 锁住收款单（核销时串行化同一张收款单的并发核销）。
func (s *Store) LockPaymentTx(ctx context.Context, tx pgx.Tx, paymentID int64) (PaymentRecord, error) {
	payment, err := scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM billing_payments WHERE id = $1 FOR UPDATE`, paymentID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentRecord{}, ErrPaymentNotFound
	}
	if err != nil {
		return PaymentRecord{}, fmt.Errorf("lock payment: %w", err)
	}
	return payment, nil
}

// GetPayment 按 id 取收款单。
func (s *Store) GetPayment(ctx context.Context, paymentID int64) (PaymentRecord, error) {
	payment, err := scanPayment(s.pool.QueryRow(ctx, `SELECT `+paymentColumns+` FROM billing_payments WHERE id = $1`, paymentID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentRecord{}, ErrPaymentNotFound
	}
	if err != nil {
		return PaymentRecord{}, fmt.Errorf("get payment: %w", err)
	}
	return payment, nil
}

// GetPaymentTx 在事务内按 id 取收款单（创建收款单后读回用，池连接看不到未提交数据）。
func (s *Store) GetPaymentTx(ctx context.Context, tx pgx.Tx, paymentID int64) (PaymentRecord, error) {
	payment, err := scanPayment(tx.QueryRow(ctx, `SELECT `+paymentColumns+` FROM billing_payments WHERE id = $1`, paymentID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentRecord{}, ErrPaymentNotFound
	}
	if err != nil {
		return PaymentRecord{}, fmt.Errorf("get payment in tx: %w", err)
	}
	return payment, nil
}

// LockOrderTx 锁住订单并读回（核销前校验金额与状态）。
func (s *Store) LockOrderTx(ctx context.Context, tx pgx.Tx, orderID int64) (Order, error) {
	order, err := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM billing_orders WHERE id = $1 FOR UPDATE`, orderID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, fmt.Errorf("lock order: %w", err)
	}
	return order, nil
}

// ListUnpaidOrdersForUpdateTx 取机构未收订单并加锁（收款自动核销用，按最早应收优先）。
func (s *Store) ListUnpaidOrdersForUpdateTx(ctx context.Context, tx pgx.Tx, tenantID int64) ([]Order, error) {
	rows, err := tx.Query(ctx, `
SELECT `+orderColumns+`
FROM billing_orders
WHERE tenant_id = $1
  AND order_type NOT IN ('gift', 'trial')
  AND status NOT IN ('cancelled', 'refunded', 'void')
  AND payable_amount > paid_amount
ORDER BY created_at ASC, id ASC
FOR UPDATE`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list unpaid orders: %w", err)
	}
	defer rows.Close()

	items := make([]Order, 0, 8)
	for rows.Next() {
		order, err := scanOrder(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan unpaid order: %w", err)
		}
		items = append(items, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unpaid orders: %w", err)
	}
	return items, nil
}

// GetExistingAllocationTx 取 (payment, order) 已有核销行并加锁，用于重复提交判幂等。
func (s *Store) GetExistingAllocationTx(ctx context.Context, tx pgx.Tx, paymentID, orderID int64) (int64, Decimal, bool, error) {
	var (
		id     int64
		amount string
	)
	err := tx.QueryRow(ctx, `
SELECT id, amount::text
FROM billing_payment_allocations
WHERE payment_id = $1 AND order_id = $2
FOR UPDATE`, paymentID, orderID).Scan(&id, &amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, fmt.Errorf("get existing allocation: %w", err)
	}
	return id, Decimal(amount), true, nil
}

// InsertAllocationTx 写一条核销明细。
func (s *Store) InsertAllocationTx(ctx context.Context, tx pgx.Tx, paymentID, orderID int64, amount Decimal, createdBy int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO billing_payment_allocations (payment_id, order_id, amount, created_by)
VALUES ($1, $2, $3::numeric, $4)
RETURNING id`, paymentID, orderID, amount.String(), nullableID(createdBy)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrAllocationConflict
		}
		return 0, fmt.Errorf("insert allocation: %w", err)
	}
	return id, nil
}

// UpdateOrderPaidTx 回写订单已收金额与状态：
// 收满 -> settled（并记 paid_at / confirmed_by）；未收满但已收 > 0 -> paid。
func (s *Store) UpdateOrderPaidTx(ctx context.Context, tx pgx.Tx, orderID int64, paidAmount Decimal, status string, paidAt *time.Time, confirmedBy int64) error {
	_, err := tx.Exec(ctx, `
UPDATE billing_orders
SET paid_amount = $2::numeric,
    status = $3,
    paid_at = $4,
    confirmed_by = $5,
    updated_at = NOW()
WHERE id = $1`, orderID, paidAmount.String(), status, paidAt, nullableID(confirmedBy))
	if err != nil {
		return fmt.Errorf("update order paid amount: %w", err)
	}
	return nil
}

// UpdatePaymentStatusTx 回写收款单状态（未核销/部分核销/已核销）。
func (s *Store) UpdatePaymentStatusTx(ctx context.Context, tx pgx.Tx, paymentID int64, status string) error {
	_, err := tx.Exec(ctx, `
UPDATE billing_payments
SET status = $2, updated_at = NOW()
WHERE id = $1`, paymentID, status)
	if err != nil {
		return fmt.Errorf("update payment status: %w", err)
	}
	return nil
}

// AllocatedTotalTx 返回收款单已核销合计。
func (s *Store) AllocatedTotalTx(ctx context.Context, tx pgx.Tx, paymentID int64) (Decimal, error) {
	var total string
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(amount), 0)::text FROM billing_payment_allocations WHERE payment_id = $1`, paymentID).Scan(&total); err != nil {
		return "", fmt.Errorf("sum payment allocations: %w", err)
	}
	return Decimal(total), nil
}

// NextPaymentNoTx 在事务内生成当日收款单号：PAY-YYYYMMDD-0001（按当天已有单号递增）。
// 并发撞号由唯一索引兜底，service 会换号重试。
func (s *Store) NextPaymentNoTx(ctx context.Context, tx pgx.Tx, day string) (string, error) {
	prefix := "PAY-" + day + "-"
	rows, err := tx.Query(ctx, `
SELECT payment_no
FROM billing_payments
WHERE payment_no LIKE $1 || '%'`, prefix)
	if err != nil {
		return "", fmt.Errorf("list daily payment numbers: %w", err)
	}
	defer rows.Close()

	maxSequence := 0
	for rows.Next() {
		var no string
		if err := rows.Scan(&no); err != nil {
			return "", fmt.Errorf("scan daily payment number: %w", err)
		}
		suffix := strings.TrimPrefix(no, prefix)
		value := 0
		if _, err := fmt.Sscanf(suffix, "%d", &value); err == nil && value > maxSequence {
			maxSequence = value
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate daily payment numbers: %w", err)
	}
	return fmt.Sprintf("%s%04d", prefix, maxSequence+1), nil
}

// ListPaymentRecords 查询收款单列表（含已核销合计与对应应收合计）。
// orgID > 0 时只看「有核销到该组织租户」或「本人录入」的收款单。
func (s *Store) ListPaymentRecords(ctx context.Context, orgID, adminID int64) ([]PaymentRecord, error) {
	rows, err := s.pool.Query(ctx, `
SELECT p.id, p.payment_no, p.payer_name, p.amount::text, to_char(p.received_on, 'YYYY-MM-DD'),
       p.method, p.voucher_no, p.status, p.note,
       COALESCE((
           SELECT SUM(a.amount)
           FROM billing_payment_allocations a
           WHERE a.payment_id = p.id
       ), 0)::text,
       COALESCE((
           SELECT SUM(o.payable_amount)
           FROM (
               SELECT DISTINCT a.order_id
               FROM billing_payment_allocations a
               WHERE a.payment_id = p.id
           ) d
           JOIN billing_orders o ON o.id = d.order_id
       ), 0)::text,
       p.created_at
FROM billing_payments p
WHERE ($1::bigint = 0
       OR p.created_by = $2
       OR EXISTS (
           SELECT 1
           FROM billing_payment_allocations a
           JOIN billing_orders o ON o.id = a.order_id
           JOIN tenant_ownerships ow ON ow.tenant_id = o.tenant_id AND ow.effective_to IS NULL
           WHERE a.payment_id = p.id AND ow.org_id = $1
       ))
ORDER BY p.received_on DESC, p.id DESC`, orgID, adminID)
	if err != nil {
		return nil, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()

	items := make([]PaymentRecord, 0, 32)
	for rows.Next() {
		var (
			item            PaymentRecord
			amount, settled string
			receivableTotal string
			createdAt       time.Time
		)
		if err := rows.Scan(&item.ID, &item.No, &item.Payer, &amount, &item.Date, &item.Method,
			&item.VoucherNo, &item.Status, &item.Note, &settled, &receivableTotal, &createdAt); err != nil {
			return nil, fmt.Errorf("scan payment: %w", err)
		}
		item.Amount = Decimal(amount)
		item.Settled = Decimal(settled)
		item.ReceivableTotal = Decimal(receivableTotal)
		item.State = paymentState(item.Status, item.Settled, item.Amount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payments: %w", err)
	}
	if len(items) > 0 {
		paymentIDs := make([]int64, 0, len(items))
		byPayment := make(map[int64][]PaymentAllocationRecord, len(items))
		for _, item := range items {
			paymentIDs = append(paymentIDs, item.ID)
		}
		allocations, err := s.listAllocationsForPayments(ctx, paymentIDs)
		if err != nil {
			return nil, err
		}
		for _, allocation := range allocations {
			byPayment[allocation.PaymentID] = append(byPayment[allocation.PaymentID], allocation)
		}
		for index := range items {
			items[index].Allocations = byPayment[items[index].ID]
			if items[index].Allocations == nil {
				items[index].Allocations = []PaymentAllocationRecord{}
			}
		}
	}
	return items, nil
}

// listAllocationsForPayments 批量取多张收款单的核销明细（带订单号与机构名）。
func (s *Store) listAllocationsForPayments(ctx context.Context, paymentIDs []int64) ([]PaymentAllocationRecord, error) {
	rows, err := s.pool.Query(ctx, `
SELECT a.id, a.payment_id, a.order_id, o.order_no, o.tenant_id, COALESCE(t.name, ''), a.amount::text
FROM billing_payment_allocations a
JOIN billing_orders o ON o.id = a.order_id
LEFT JOIN tenants t ON t.id = o.tenant_id
WHERE a.payment_id = ANY($1)
ORDER BY a.payment_id, a.id`, paymentIDs)
	if err != nil {
		return nil, fmt.Errorf("list payment allocations for payments: %w", err)
	}
	defer rows.Close()

	items := make([]PaymentAllocationRecord, 0, len(paymentIDs)*2)
	for rows.Next() {
		var (
			item   PaymentAllocationRecord
			amount string
		)
		if err := rows.Scan(&item.ID, &item.PaymentID, &item.OrderID, &item.OrderNo,
			&item.TenantID, &item.TenantName, &amount); err != nil {
			return nil, fmt.Errorf("scan payment allocation: %w", err)
		}
		item.Amount = Decimal(amount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment allocations: %w", err)
	}
	return items, nil
}

// GetPaymentRecord 取单张收款单的展示行（含核销合计与对应应收合计）。
func (s *Store) GetPaymentRecord(ctx context.Context, paymentID int64) (PaymentRecord, error) {
	var (
		item            PaymentRecord
		amount, settled string
		receivableTotal string
	)
	err := s.pool.QueryRow(ctx, `
SELECT p.id, p.payment_no, p.payer_name, p.amount::text, to_char(p.received_on, 'YYYY-MM-DD'),
       p.method, p.voucher_no, p.status, p.note,
       COALESCE((
           SELECT SUM(a.amount)
           FROM billing_payment_allocations a
           WHERE a.payment_id = p.id
       ), 0)::text,
       COALESCE((
           SELECT SUM(o.payable_amount)
           FROM (
               SELECT DISTINCT a.order_id
               FROM billing_payment_allocations a
               WHERE a.payment_id = p.id
           ) d
           JOIN billing_orders o ON o.id = d.order_id
       ), 0)::text
FROM billing_payments p
WHERE p.id = $1`, paymentID).Scan(&item.ID, &item.No, &item.Payer, &amount, &item.Date, &item.Method,
		&item.VoucherNo, &item.Status, &item.Note, &settled, &receivableTotal)
	if errors.Is(err, pgx.ErrNoRows) {
		return PaymentRecord{}, ErrPaymentNotFound
	}
	if err != nil {
		return PaymentRecord{}, fmt.Errorf("get payment record: %w", err)
	}
	item.Amount = Decimal(amount)
	item.Settled = Decimal(settled)
	item.ReceivableTotal = Decimal(receivableTotal)
	item.State = paymentState(item.Status, item.Settled, item.Amount)
	return item, nil
}

// ListPaymentAllocations 返回一张收款单的核销明细（带订单号与机构名）。
func (s *Store) ListPaymentAllocations(ctx context.Context, paymentID int64) ([]PaymentAllocationRecord, error) {
	rows, err := s.pool.Query(ctx, `
SELECT a.id, a.payment_id, a.order_id, o.order_no, o.tenant_id, COALESCE(t.name, ''), a.amount::text
FROM billing_payment_allocations a
JOIN billing_orders o ON o.id = a.order_id
LEFT JOIN tenants t ON t.id = o.tenant_id
WHERE a.payment_id = $1
ORDER BY a.id`, paymentID)
	if err != nil {
		return nil, fmt.Errorf("list payment allocations: %w", err)
	}
	defer rows.Close()

	items := make([]PaymentAllocationRecord, 0, 8)
	for rows.Next() {
		var (
			item   PaymentAllocationRecord
			amount string
		)
		if err := rows.Scan(&item.ID, &item.PaymentID, &item.OrderID, &item.OrderNo, &item.TenantID, &item.TenantName, &amount); err != nil {
			return nil, fmt.Errorf("scan payment allocation: %w", err)
		}
		item.Amount = Decimal(amount)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment allocations: %w", err)
	}
	return items, nil
}

// OrderTenantIDs 批量取订单所属租户（跨租户核销的数据权限校验用）。
func (s *Store) OrderTenantIDs(ctx context.Context, orderIDs []int64) (map[int64]int64, error) {
	result := make(map[int64]int64, len(orderIDs))
	if len(orderIDs) == 0 {
		return result, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id FROM billing_orders WHERE id = ANY($1)`, orderIDs)
	if err != nil {
		return nil, fmt.Errorf("query order tenants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			orderID  int64
			tenantID int64
		)
		if err := rows.Scan(&orderID, &tenantID); err != nil {
			return nil, fmt.Errorf("scan order tenant: %w", err)
		}
		result[orderID] = tenantID
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate order tenants: %w", err)
	}
	return result, nil
}

// paymentState 把库里的收款单状态映射成前端展示状态。
func paymentState(status string, settled, amount Decimal) string {
	switch normalizeEnum(status) {
	case PaymentStatusVoid:
		return paymentStateVoid
	case PaymentStatusAllocated:
		return paymentStateAllocated
	case PaymentStatusPartial:
		return paymentStatePartial
	}
	settledCents, err := parseAmountCents(settled)
	if err != nil {
		settledCents = 0
	}
	amountCents, err := parseAmountCents(amount)
	if err != nil {
		amountCents = 0
	}
	switch {
	case settledCents <= 0:
		return paymentStateUnallocated
	case settledCents >= amountCents:
		return paymentStateAllocated
	default:
		return paymentStatePartial
	}
}

// derivePaymentStatus 根据已核销合计推导收款单状态。
func derivePaymentStatus(settled, amount Decimal) string {
	settledCents, err := parseAmountCents(settled)
	if err != nil {
		return PaymentStatusUnallocated
	}
	amountCents, err := parseAmountCents(amount)
	if err != nil {
		return PaymentStatusUnallocated
	}
	switch {
	case settledCents <= 0:
		return PaymentStatusUnallocated
	case settledCents >= amountCents:
		return PaymentStatusAllocated
	default:
		return PaymentStatusPartial
	}
}

// ListAllocatableOrders 列出机构可核销订单：已收 < 应收，且订单未作废/取消/退款。
// 新建跨机构收款单的订单选择器用它带出订单号与剩余应收。
func (s *Store) ListAllocatableOrders(ctx context.Context, tenantID int64, limit int) ([]Order, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
SELECT `+orderColumns+`
FROM billing_orders
WHERE tenant_id = $1
  AND order_type NOT IN ('gift', 'trial')
  AND status NOT IN ('cancelled', 'refunded', 'void')
  AND payable_amount > paid_amount
ORDER BY created_at ASC, id ASC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list allocatable orders: %w", err)
	}
	defer rows.Close()

	items := make([]Order, 0, 16)
	for rows.Next() {
		order, err := scanOrder(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan allocatable order: %w", err)
		}
		items = append(items, order)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate allocatable orders: %w", err)
	}
	return items, nil
}
