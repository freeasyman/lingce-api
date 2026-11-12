package billing

// 财务三张清单的导出查询：按数据权限与日期范围取行，列顺序与 financeExportHeader 一致。

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FinanceExportQuery 是导出查询条件（service 已完成权限与日期校验）。
type FinanceExportQuery struct {
	Kind        string
	TenantID    int64
	OrgID       int64
	AdminID     int64
	From        *time.Time
	ToExclusive *time.Time
}

// ListFinanceExportRows 按清单类型查询导出数据，limit 用于判断是否超过行数上限。
func (s *Store) ListFinanceExportRows(ctx context.Context, query FinanceExportQuery, limit int) ([][]string, error) {
	if limit <= 0 {
		limit = MaxFinanceExportRows + 1
	}
	switch normalizeEnum(query.Kind) {
	case FinanceExportReceivables:
		return s.listReceivablesExportRows(ctx, query, limit)
	case FinanceExportPayments:
		return s.listPaymentsExportRows(ctx, query, limit)
	case FinanceExportInvoices:
		return s.listInvoicesExportRows(ctx, query, limit)
	default:
		return nil, fmt.Errorf("unsupported finance export kind %q", query.Kind)
	}
}

// financeExportWhere 拼装三种清单共享的 tenant/org/from/to 过滤条件。
func financeExportWhere(alias, tenantColumn, timeColumn string, query FinanceExportQuery, args *[]any, conditions *[]string) {
	if query.TenantID > 0 {
		*args = append(*args, query.TenantID)
		*conditions = append(*conditions, fmt.Sprintf("%s.%s = $%d", alias, tenantColumn, len(*args)))
	}
	if query.From != nil {
		*args = append(*args, *query.From)
		*conditions = append(*conditions, fmt.Sprintf("%s.%s >= $%d", alias, timeColumn, len(*args)))
	}
	if query.ToExclusive != nil {
		*args = append(*args, *query.ToExclusive)
		*conditions = append(*conditions, fmt.Sprintf("%s.%s < $%d", alias, timeColumn, len(*args)))
	}
}

func (s *Store) listReceivablesExportRows(ctx context.Context, query FinanceExportQuery, limit int) ([][]string, error) {
	conditions := []string{
		"o.order_type NOT IN ('gift', 'trial')",
		"o.status NOT IN ('cancelled', 'void', 'refunded')",
	}
	args := make([]any, 0, 4)
	financeExportWhere("o", "tenant_id", "created_at", query, &args, &conditions)
	if query.OrgID > 0 {
		args = append(args, query.OrgID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
            SELECT 1 FROM tenant_ownerships ow
            WHERE ow.tenant_id = o.tenant_id
              AND ow.effective_to IS NULL
              AND ow.org_id = $%d
        )`, len(args)))
	}
	args = append(args, limit)
	sql := `
SELECT o.order_no, o.tenant_id, COALESCE(t.name, ''), o.order_type,
       o.payable_amount::text, o.paid_amount::text, (o.payable_amount - o.paid_amount)::text,
       o.status, to_char(o.created_at, 'YYYY-MM-DD HH24:MI')
FROM billing_orders o
LEFT JOIN tenants t ON t.id = o.tenant_id
WHERE ` + strings.Join(conditions, " AND ") + `
ORDER BY o.created_at DESC, o.id DESC
LIMIT $` + fmt.Sprint(len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("export receivables: %w", err)
	}
	defer rows.Close()
	result := make([][]string, 0, 64)
	for rows.Next() {
		var orderNo, tenantName, orderType, payable, paid, unpaid, status, createdAt string
		var tenantID int64
		if err := rows.Scan(&orderNo, &tenantID, &tenantName, &orderType, &payable, &paid, &unpaid, &status, &createdAt); err != nil {
			return nil, fmt.Errorf("scan receivable export row: %w", err)
		}
		result = append(result, []string{
			orderNo, fmt.Sprint(tenantID), tenantName, orderTypeCSVLabel(orderType),
			payable, paid, unpaid, orderStatusCSVLabel(status), createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate receivable export rows: %w", err)
	}
	return result, nil
}

func (s *Store) listPaymentsExportRows(ctx context.Context, query FinanceExportQuery, limit int) ([][]string, error) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 4)
	if query.TenantID > 0 {
		args = append(args, query.TenantID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
            SELECT 1 FROM billing_payment_allocations a
            JOIN billing_orders o ON o.id = a.order_id
            WHERE a.payment_id = p.id AND o.tenant_id = $%d
        )`, len(args)))
	}
	if query.From != nil {
		args = append(args, *query.From)
		conditions = append(conditions, fmt.Sprintf("p.received_on >= $%d::date", len(args)))
	}
	if query.ToExclusive != nil {
		args = append(args, *query.ToExclusive)
		conditions = append(conditions, fmt.Sprintf("p.received_on < $%d::date", len(args)))
	}
	if query.OrgID > 0 {
		args = append(args, query.OrgID, query.AdminID)
		conditions = append(conditions, fmt.Sprintf(`(p.created_by = $%d OR EXISTS (
            SELECT 1 FROM billing_payment_allocations a
            JOIN billing_orders o ON o.id = a.order_id
            JOIN tenant_ownerships ow ON ow.tenant_id = o.tenant_id AND ow.effective_to IS NULL
            WHERE a.payment_id = p.id AND ow.org_id = $%d
        ))`, len(args), len(args)-1))
	}
	args = append(args, limit)
	sql := `
SELECT p.payment_no, p.payer_name, p.amount::text,
       to_char(p.received_on, 'YYYY-MM-DD'), p.method, p.voucher_no,
       COALESCE((SELECT SUM(a.amount) FROM billing_payment_allocations a WHERE a.payment_id = p.id), 0)::text,
       (p.amount - COALESCE((SELECT SUM(a.amount) FROM billing_payment_allocations a WHERE a.payment_id = p.id), 0))::text,
       p.status, p.note, to_char(p.created_at, 'YYYY-MM-DD HH24:MI')
FROM billing_payments p
WHERE ` + strings.Join(conditions, " AND ") + `
ORDER BY p.received_on DESC, p.id DESC
LIMIT $` + fmt.Sprint(len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("export payments: %w", err)
	}
	defer rows.Close()
	result := make([][]string, 0, 64)
	for rows.Next() {
		var paymentNo, payer, amount, receivedOn, method, voucherNo, allocated, unallocated, status, note, createdAt string
		if err := rows.Scan(&paymentNo, &payer, &amount, &receivedOn, &method, &voucherNo,
			&allocated, &unallocated, &status, &note, &createdAt); err != nil {
			return nil, fmt.Errorf("scan payment export row: %w", err)
		}
		result = append(result, []string{
			paymentNo, payer, amount, receivedOn, method, voucherNo,
			allocated, unallocated, paymentStatusCSVLabel(status), note, createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment export rows: %w", err)
	}
	return result, nil
}

func (s *Store) listInvoicesExportRows(ctx context.Context, query FinanceExportQuery, limit int) ([][]string, error) {
	conditions := []string{"1 = 1"}
	args := make([]any, 0, 4)
	financeExportWhere("i", "tenant_id", "created_at", query, &args, &conditions)
	if query.OrgID > 0 {
		args = append(args, query.OrgID)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
            SELECT 1 FROM tenant_ownerships ow
            WHERE ow.tenant_id = i.tenant_id
              AND ow.effective_to IS NULL
              AND ow.org_id = $%d
        )`, len(args)))
	}
	args = append(args, limit)
	sql := `
SELECT i.invoice_no, i.tenant_id, COALESCE(t.name, ''), COALESCE(o.order_no, ''),
       i.amount::text, i.invoice_type, i.title, i.tax_no, i.status,
       COALESCE(to_char(i.issued_on, 'YYYY-MM-DD'), ''), i.note,
       to_char(i.created_at, 'YYYY-MM-DD HH24:MI')
FROM billing_invoices i
LEFT JOIN tenants t ON t.id = i.tenant_id
LEFT JOIN billing_orders o ON o.id = i.order_id
WHERE ` + strings.Join(conditions, " AND ") + `
ORDER BY i.created_at DESC, i.id DESC
LIMIT $` + fmt.Sprint(len(args))

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("export invoices: %w", err)
	}
	defer rows.Close()
	result := make([][]string, 0, 64)
	for rows.Next() {
		var invoiceNo, tenantName, orderNo, amount, invoiceType, title, taxNo, status, issuedOn, note, createdAt string
		var tenantID int64
		if err := rows.Scan(&invoiceNo, &tenantID, &tenantName, &orderNo, &amount, &invoiceType,
			&title, &taxNo, &status, &issuedOn, &note, &createdAt); err != nil {
			return nil, fmt.Errorf("scan invoice export row: %w", err)
		}
		result = append(result, []string{
			invoiceNo, fmt.Sprint(tenantID), tenantName, orderNo, amount, invoiceType,
			title, taxNo, invoiceStatusLabel(status), issuedOn, note, createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invoice export rows: %w", err)
	}
	return result, nil
}

// orderStatusCSVLabel 订单状态中文名。
func orderStatusCSVLabel(status string) string {
	switch normalizeEnum(status) {
	case OrderStatusCreated:
		return "待收款"
	case OrderStatusPaid:
		return "部分收款"
	case OrderStatusSettled:
		return "已结清"
	case OrderStatusRefunded:
		return "已退款"
	case OrderStatusCancelled:
		return "已取消"
	case OrderStatusVoid:
		return "已作废"
	default:
		return status
	}
}

// orderTypeCSVLabel 订单类型中文名。
func orderTypeCSVLabel(orderType string) string {
	switch normalizeEnum(orderType) {
	case OrderTypeSingle:
		return "单品"
	case OrderTypeBundle:
		return "套餐"
	case OrderTypeIncrement:
		return "增量"
	case OrderTypeHardware:
		return "硬件"
	case OrderTypeGift:
		return "赠送"
	case OrderTypeTrial:
		return "试用"
	default:
		return orderType
	}
}

// paymentStatusCSVLabel 收款单状态中文名。
func paymentStatusCSVLabel(status string) string {
	switch normalizeEnum(status) {
	case PaymentStatusUnallocated:
		return "未核销"
	case PaymentStatusPartial:
		return "部分核销"
	case PaymentStatusAllocated:
		return "已核销"
	case PaymentStatusVoid:
		return "已作废"
	default:
		return status
	}
}
