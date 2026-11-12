package billing

// 开票记录的数据访问层。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// invoiceColumns 是开票记录的公共列：金额转 text 交给 Decimal，日期/时间统一在 SQL 里格式化。
const invoiceColumns = `i.id, i.invoice_no, i.tenant_id, COALESCE(t.name, ''),
       i.order_id, COALESCE(o.order_no, ''),
       i.amount::text, i.invoice_type, i.title, i.tax_no, i.status,
       COALESCE(to_char(i.issued_on, 'YYYY-MM-DD'), ''), i.note, i.created_by,
       to_char(i.created_at, 'YYYY-MM-DD HH24:MI'), to_char(i.updated_at, 'YYYY-MM-DD HH24:MI')`

const invoiceFrom = ` FROM billing_invoices i
LEFT JOIN tenants t ON t.id = i.tenant_id
LEFT JOIN billing_orders o ON o.id = i.order_id`

// scanInvoice 解析一行开票记录（含机构名与订单号）。
func scanInvoice(scan func(dest ...any) error) (Invoice, error) {
	var (
		item        Invoice
		orderID     *int64
		amount      string
		createdBy   *int64
		status      string
		invoiceNo   string
		invoiceType string
		title       string
		taxNo       string
		issuedOn    string
		note        string
		createdAt   string
		updatedAt   string
		tenantID    int64
		tenantName  string
		orderNo     string
	)
	if err := scan(
		&item.ID, &invoiceNo, &tenantID, &tenantName,
		&orderID, &orderNo,
		&amount, &invoiceType, &title, &taxNo, &status,
		&issuedOn, &note, &createdBy,
		&createdAt, &updatedAt,
	); err != nil {
		return Invoice{}, err
	}
	item.InvoiceNo = invoiceNo
	item.TenantID = tenantID
	item.TenantName = tenantName
	item.OrderID = orderID
	item.OrderNo = orderNo
	item.Amount = Decimal(amount)
	item.InvoiceType = invoiceType
	item.Title = title
	item.TaxNo = taxNo
	item.Status = status
	item.IssuedOn = issuedOn
	item.Note = note
	item.CreatedBy = createdBy
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	return item, nil
}

// InsertInvoiceTx 在事务内写入一张待开票记录；发票号非空时由部分唯一索引兜底全局唯一。
//
// 关联订单的开票额度校验与插入必须在同一事务内（调用方已锁订单行），
// 否则并发开票会同时通过「累计金额 <= 应收」检查。
func (s *Store) InsertInvoiceTx(ctx context.Context, tx pgx.Tx, req CreateInvoiceRequest, createdBy int64) (int64, error) {
	var orderID any
	if req.OrderID > 0 {
		orderID = req.OrderID
	}
	var id int64
	err := tx.QueryRow(ctx, `
INSERT INTO billing_invoices (invoice_no, tenant_id, order_id, amount, invoice_type, title, tax_no,
                              status, issued_on, note, created_by)
VALUES ($1, $2, $3, $4::numeric, $5, $6, $7, 'applied', NULL, $8, $9)
RETURNING id`,
		req.InvoiceNo, req.TenantID, orderID, req.Amount.String(), req.InvoiceType, req.Title, req.TaxNo,
		req.Note, nullableID(createdBy)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrInvoiceNoExists
		}
		return 0, fmt.Errorf("insert invoice: %w", err)
	}
	return id, nil
}

// InsertInvoice 写入一张待开票记录（无订单额度校验的单条入口，主要供测试/工具使用）。
func (s *Store) InsertInvoice(ctx context.Context, req CreateInvoiceRequest, createdBy int64) (Invoice, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Invoice{}, fmt.Errorf("begin insert invoice: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := s.InsertInvoiceTx(ctx, tx, req, createdBy)
	if err != nil {
		return Invoice{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Invoice{}, fmt.Errorf("commit insert invoice: %w", err)
	}
	return s.GetInvoice(ctx, id)
}

// GetInvoice 按 id 取开票记录（不存在返回 ErrInvoiceNotFound）。
func (s *Store) GetInvoice(ctx context.Context, invoiceID int64) (Invoice, error) {
	invoice, err := scanInvoice(s.pool.QueryRow(ctx,
		`SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1`, invoiceID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrInvoiceNotFound
	}
	if err != nil {
		return Invoice{}, fmt.Errorf("get invoice: %w", err)
	}
	return invoice, nil
}

// LockInvoiceTx 锁住一张开票记录（标记已开/作废时串行化）。
func (s *Store) LockInvoiceTx(ctx context.Context, tx pgx.Tx, invoiceID int64) (Invoice, error) {
	invoice, err := scanInvoice(tx.QueryRow(ctx,
		`SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1 FOR UPDATE OF i`, invoiceID).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrInvoiceNotFound
	}
	if err != nil {
		return Invoice{}, fmt.Errorf("lock invoice: %w", err)
	}
	return invoice, nil
}

// ListInvoices 按机构/状态分页查询开票记录（新→旧），返回当页与总条数。
func (s *Store) ListInvoices(ctx context.Context, filter InvoiceFilter) ([]Invoice, int64, error) {
	const limit = 500
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 4)
	if filter.TenantID > 0 {
		args = append(args, filter.TenantID)
		conditions = append(conditions, fmt.Sprintf("i.tenant_id = $%d", len(args)))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		conditions = append(conditions, fmt.Sprintf("i.status = $%d", len(args)))
	}
	if filter.OrgID > 0 {
		args = append(args, filter.OrgID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
            SELECT 1 FROM tenant_ownerships ow
            WHERE ow.tenant_id = i.tenant_id
              AND ow.effective_to IS NULL
              AND ow.org_id = $%d
        )`, len(args)))
	}
	where := strings.Join(conditions, " AND ")

	var total int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM billing_invoices i WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count invoices: %w", err)
	}

	pageArgs := append(append(make([]any, 0, len(args)+1), args...), limit)
	rows, err := s.pool.Query(ctx, `SELECT `+invoiceColumns+invoiceFrom+`
WHERE `+where+`
ORDER BY i.created_at DESC, i.id DESC
LIMIT $`+fmt.Sprint(len(args)+1), pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()

	items := make([]Invoice, 0, 32)
	for rows.Next() {
		invoice, err := scanInvoice(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan invoice: %w", err)
		}
		items = append(items, invoice)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate invoices: %w", err)
	}
	return items, total, nil
}

// SumInvoiceAmountsByOrderTx 统计订单已开票金额（不含作废发票），用于累计开票额度校验。
func (s *Store) SumInvoiceAmountsByOrderTx(ctx context.Context, tx pgx.Tx, orderID int64) (Decimal, error) {
	var total string
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(amount), 0)::text
FROM billing_invoices
WHERE order_id = $1 AND status <> 'voided'`, orderID).Scan(&total); err != nil {
		return "", fmt.Errorf("sum invoice amounts: %w", err)
	}
	return Decimal(total), nil
}

// UpdateInvoiceIssueTx 标记已开：写入发票号与开票日期，条件更新兜住并发重复标记。
func (s *Store) UpdateInvoiceIssueTx(ctx context.Context, tx pgx.Tx, invoiceID int64, invoiceNo, issuedOn string) (Invoice, error) {
	invoice, err := scanInvoice(tx.QueryRow(ctx, `
WITH updated AS (
    UPDATE billing_invoices
    SET invoice_no = $2,
        issued_on = $3::date,
        status = 'issued',
        updated_at = NOW()
    WHERE id = $1 AND status = 'applied'
    RETURNING *
)
SELECT `+invoiceColumns+`
FROM updated i
LEFT JOIN tenants t ON t.id = i.tenant_id
LEFT JOIN billing_orders o ON o.id = i.order_id`, invoiceID, invoiceNo, issuedOn).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrInvoiceStatusInvalid
	}
	if err != nil {
		if isUniqueViolation(err) {
			return Invoice{}, ErrInvoiceNoExists
		}
		return Invoice{}, fmt.Errorf("issue invoice: %w", err)
	}
	return invoice, nil
}

// VoidInvoiceTx 作废发票：只允许 applied/issued；已作废重复提交由 service 幂等返回。
func (s *Store) VoidInvoiceTx(ctx context.Context, tx pgx.Tx, invoiceID int64, note string) (Invoice, error) {
	invoice, err := scanInvoice(tx.QueryRow(ctx, `
WITH updated AS (
    UPDATE billing_invoices
    SET status = 'voided',
        note = CASE WHEN $2 = '' THEN note ELSE $2 END,
        updated_at = NOW()
    WHERE id = $1 AND status IN ('applied', 'issued')
    RETURNING *
)
SELECT `+invoiceColumns+`
FROM updated i
LEFT JOIN tenants t ON t.id = i.tenant_id
LEFT JOIN billing_orders o ON o.id = i.order_id`, invoiceID, note).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrInvoiceStatusInvalid
	}
	if err != nil {
		return Invoice{}, fmt.Errorf("void invoice: %w", err)
	}
	return invoice, nil
}

// ListInvoiceableOrders 列出机构可开票订单：未作废/取消/退款，且累计开票金额小于应收。
func (s *Store) ListInvoiceableOrders(ctx context.Context, tenantID int64, limit int) ([]InvoiceableOrder, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
SELECT o.id, o.order_no, o.order_type, o.payable_amount::text,
       COALESCE(invoiced.total, 0)::text, o.status,
       to_char(o.created_at, 'YYYY-MM-DD')
FROM billing_orders o
LEFT JOIN LATERAL (
    SELECT SUM(i.amount) AS total
    FROM billing_invoices i
    WHERE i.order_id = o.id AND i.status <> 'voided'
) invoiced ON TRUE
WHERE o.tenant_id = $1
  AND o.order_type NOT IN ('gift', 'trial')
  AND o.status NOT IN ('void', 'cancelled', 'refunded')
  AND o.payable_amount > COALESCE(invoiced.total, 0)
ORDER BY o.created_at ASC, o.id ASC
LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list invoiceable orders: %w", err)
	}
	defer rows.Close()

	items := make([]InvoiceableOrder, 0, 16)
	for rows.Next() {
		var (
			item              InvoiceableOrder
			payable, invoiced string
		)
		if err := rows.Scan(&item.ID, &item.OrderNo, &item.OrderType, &payable, &invoiced,
			&item.Status, &item.OrderCreatedAt); err != nil {
			return nil, fmt.Errorf("scan invoiceable order: %w", err)
		}
		payableCents, err := parseAmountCents(Decimal(payable))
		if err != nil {
			return nil, fmt.Errorf("parse payable: %w", err)
		}
		invoicedCents, err := parseAmountCents(Decimal(invoiced))
		if err != nil {
			return nil, fmt.Errorf("parse invoiced: %w", err)
		}
		item.PayableAmount = Decimal(payable)
		item.InvoicedAmount = Decimal(invoiced)
		item.Invoiceable = centsToDecimal(payableCents - invoicedCents)
		item.InvoiceableText = item.Invoiceable.String()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invoiceable orders: %w", err)
	}
	return items, nil
}
