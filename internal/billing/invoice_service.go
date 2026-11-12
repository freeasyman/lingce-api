package billing

// 开票记录的服务层。
//
// 关键规则：
//   - 关联订单时锁订单行后再统计已开票金额，保证并发开票不会超过订单应收；
//   - 已作废 / 已取消 / 已退款订单不允许开票；
//   - 标记已开与作废都做幂等处理，重复提交不重复写日志；
//   - 发票号非空时全局唯一，重复返回 409。

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// ListInvoices 查询开票记录（运营端按数据权限收敛；机构档案按 tenantID 过滤）。
func (s *InstitutionService) ListInvoices(ctx context.Context, filter InvoiceFilter, adminID int64) (*InvoiceListResult, error) {
	filter, err := normalizeInvoiceFilter(filter)
	if err != nil {
		return nil, err
	}
	if filter.TenantID > 0 {
		if err := s.requireTenantAccess(ctx, filter.TenantID, adminID); err != nil {
			return nil, err
		}
	} else {
		scope, err := s.store.GetAdminScope(ctx, adminID)
		if err != nil {
			return nil, err
		}
		if !scope.CanSeeAll {
			if scope.OrgID == nil || *scope.OrgID <= 0 {
				return nil, ErrAdminScopeMissing
			}
			filter.OrgID = *scope.OrgID
		}
	}
	items, total, err := s.store.ListInvoices(ctx, filter)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []Invoice{}
	}
	return &InvoiceListResult{Items: items, Total: total, IsMock: false}, nil
}

// ListInvoiceableOrders 列出机构可开票订单（含剩余可开票金额）。
func (s *InstitutionService) ListInvoiceableOrders(ctx context.Context, tenantID, adminID int64) ([]InvoiceableOrder, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	items, err := s.store.ListInvoiceableOrders(ctx, tenantID, 200)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []InvoiceableOrder{}
	}
	return items, nil
}

// CreateInvoice 新建待开票记录。
func (s *InstitutionService) CreateInvoice(ctx context.Context, req CreateInvoiceRequest, adminID int64) (*Invoice, error) {
	req, err := req.normalize()
	if err != nil {
		return nil, err
	}
	if err := s.requireTenantAccess(ctx, req.TenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, req.TenantID)
	if err != nil {
		return nil, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create invoice transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var order Order
	if req.OrderID > 0 {
		order, err = s.store.LockOrderTx(ctx, tx, req.OrderID)
		if err != nil {
			return nil, err
		}
		if order.TenantID != req.TenantID {
			return nil, invalidf("订单不属于所选机构")
		}
		if err := invoiceableOrderGuard(order); err != nil {
			return nil, err
		}
		invoiced, err := s.store.SumInvoiceAmountsByOrderTx(ctx, tx, order.ID)
		if err != nil {
			return nil, err
		}
		payableCents, err := parseAmountCents(order.PayableAmount)
		if err != nil {
			return nil, fmt.Errorf("parse order payable: %w", err)
		}
		invoicedCents, err := parseAmountCents(invoiced)
		if err != nil {
			return nil, fmt.Errorf("parse invoiced amount: %w", err)
		}
		remaining := payableCents - invoicedCents
		if remaining < 0 {
			remaining = 0
		}
		if strings.TrimSpace(string(req.Amount)) == "" {
			req.Amount = centsToDecimal(remaining)
		}
		amountCents, err := parseAmountCents(req.Amount)
		if err != nil {
			return nil, invalidf("开票金额 %s", err)
		}
		if amountCents <= 0 {
			return nil, invalidf("该订单已开满，不能继续开票")
		}
		if amountCents > remaining {
			return nil, invalidf("开票金额不能超过订单应收：该订单剩余可开票 %s", centsToDecimal(remaining))
		}
		req.Amount = centsToDecimal(amountCents)
	}
	if strings.TrimSpace(string(req.Amount)) == "" {
		return nil, invalidf("开票金额要大于 0")
	}

	invoiceID, err := s.store.InsertInvoiceTx(ctx, tx, req, adminID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create invoice transaction: %w", err)
	}

	invoice, err := s.store.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
		TenantID: invoice.TenantID, TenantName: base.Name, ActorID: adminID,
		LogType: "billing", ActionCode: "billing.invoice.create", ActionName: "新建开票记录",
		ObjectType: "invoice", ObjectID: strconv.FormatInt(invoice.ID, 10), ObjectName: invoice.InvoiceNo,
		After: map[string]any{
			"invoiceNo": invoice.InvoiceNo, "amount": invoice.Amount.String(),
			"invoiceType": invoice.InvoiceType, "orderId": req.OrderID,
			"title": invoice.Title, "taxNo": invoice.TaxNo,
			"detail": fmt.Sprintf("开票金额 %s · %s", invoice.Amount.String(), invoice.InvoiceType),
		},
	})
	return &invoice, nil
}

// IssueInvoice 标记已开：写发票号与开票日期。
//
// 已开票重复提交按幂等处理；发票号/日期不一致返回 409。
func (s *InstitutionService) IssueInvoice(ctx context.Context, invoiceID int64, req IssueInvoiceRequest, adminID int64) (*Invoice, error) {
	req, err := req.normalize()
	if err != nil {
		return nil, err
	}
	existing, err := s.store.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if err := s.requireTenantAccess(ctx, existing.TenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, existing.TenantID)
	if err != nil {
		return nil, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin issue invoice transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invoice, err := s.store.LockInvoiceTx(ctx, tx, invoiceID)
	if err != nil {
		return nil, err
	}
	switch normalizeEnum(invoice.Status) {
	case InvoiceStatusVoided:
		return nil, &ConflictError{Sentinel: ErrInvoiceStatusInvalid, Message: "发票已作废，不能标记已开"}
	case InvoiceStatusIssued:
		if req.InvoiceNo != "" && req.InvoiceNo != invoice.InvoiceNo {
			return nil, &ConflictError{Sentinel: ErrInvoiceStatusInvalid, Message: "发票已开票，发票号与本次不一致"}
		}
		if req.IssuedOn != "" && req.IssuedOn != invoice.IssuedOn {
			return nil, &ConflictError{Sentinel: ErrInvoiceStatusInvalid, Message: "发票已开票，开票日期与本次不一致"}
		}
		return &invoice, nil
	}

	issuedOn := req.IssuedOn
	if issuedOn == "" {
		issuedOn = s.today().Format("2006-01-02")
	}
	invoiceNo := firstNonEmpty(req.InvoiceNo, invoice.InvoiceNo, invoiceNumberFor(invoice.ID, issuedOn))
	updated, err := s.store.UpdateInvoiceIssueTx(ctx, tx, invoiceID, invoiceNo, issuedOn)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit issue invoice transaction: %w", err)
	}
	s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
		TenantID: updated.TenantID, TenantName: base.Name, ActorID: adminID,
		LogType: "billing", ActionCode: "billing.invoice.issue", ActionName: "标记发票已开",
		ObjectType: "invoice", ObjectID: strconv.FormatInt(updated.ID, 10), ObjectName: updated.InvoiceNo,
		Before: map[string]any{"status": invoice.Status, "invoiceNo": invoice.InvoiceNo},
		After: map[string]any{
			"status": updated.Status, "invoiceNo": updated.InvoiceNo, "issuedOn": updated.IssuedOn,
			"detail": fmt.Sprintf("发票号 %s · 开票日期 %s", updated.InvoiceNo, updated.IssuedOn),
		},
	})
	return &updated, nil
}

// VoidInvoice 作废发票；已作废重复提交幂等返回。
func (s *InstitutionService) VoidInvoice(ctx context.Context, invoiceID int64, req VoidInvoiceRequest, adminID int64) (*Invoice, error) {
	note := strings.TrimSpace(req.Note)
	if len([]rune(note)) > 500 {
		return nil, invalidf("备注过长")
	}
	existing, err := s.store.GetInvoice(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if err := s.requireTenantAccess(ctx, existing.TenantID, adminID); err != nil {
		return nil, err
	}
	if normalizeEnum(existing.Status) == InvoiceStatusVoided {
		return &existing, nil
	}
	base, err := s.store.GetInstitutionBase(ctx, existing.TenantID)
	if err != nil {
		return nil, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin void invoice transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invoice, err := s.store.LockInvoiceTx(ctx, tx, invoiceID)
	if err != nil {
		return nil, err
	}
	if normalizeEnum(invoice.Status) == InvoiceStatusVoided {
		return &invoice, nil
	}
	updated, err := s.store.VoidInvoiceTx(ctx, tx, invoiceID, note)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit void invoice transaction: %w", err)
	}
	s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
		TenantID: updated.TenantID, TenantName: base.Name, ActorID: adminID,
		LogType: "billing", ActionCode: "billing.invoice.void", ActionName: "作废发票",
		ObjectType: "invoice", ObjectID: strconv.FormatInt(updated.ID, 10), ObjectName: updated.InvoiceNo,
		Before: map[string]any{"status": invoice.Status},
		After:  map[string]any{"status": updated.Status, "note": note},
	})
	return &updated, nil
}

// invoiceableOrderGuard 校验订单状态是否允许开票。
func invoiceableOrderGuard(order Order) error {
	switch normalizeEnum(order.Status) {
	case OrderStatusVoid:
		return &ConflictError{Sentinel: ErrOrderNotInvoiceable, Message: "订单已作废，不能开票"}
	case OrderStatusCancelled:
		return &ConflictError{Sentinel: ErrOrderNotInvoiceable, Message: "订单已取消，不能开票"}
	case OrderStatusRefunded:
		return &ConflictError{Sentinel: ErrOrderNotInvoiceable, Message: "订单已退款，不能开票"}
	default:
		return nil
	}
}
