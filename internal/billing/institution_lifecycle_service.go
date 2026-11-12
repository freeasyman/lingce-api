package billing

// 计费运营端第 2 批：暂停服务 / 恢复服务 / 作废订单 / 人工调额度。
//
// 业务口径（MVP）：
//   · 暂停 = 临时停用，服务期不延长、额度不变；客户仍可登录查看数据，
//     但扣量/预检返回 SUBSCRIPTION_PAUSED（生成与消耗被拒绝）。
//   · 恢复 = 清除暂停标记，其余字段不动（到期日不会因暂停顺延）。
//   · 作废订单 = 只允许作废未收款且未核销的订单；已作废再作废幂等返回 200。
//   · 人工调额度 = granted/balance 同步加减（used 不动），负向不能把余额减成负数；
//     写 source=correction 流水，审批人口径与赠送一致（1 个增量包起需要审批人）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// SuspendInstitution 暂停服务。重复暂停幂等返回当前档案。
func (s *InstitutionService) SuspendInstitution(ctx context.Context, tenantID int64, req SuspendRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	if base.PeriodID == nil {
		return nil, &ConflictError{Sentinel: ErrInstitutionNotOpened, Message: "机构尚未开通，不能暂停"}
	}
	if base.PausedAt != nil {
		// 重复暂停：不报错、不改数据，直接返回当前档案。
		return s.GetInstitution(ctx, tenantID)
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin suspend transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	paused, err := s.store.PauseTenantTx(ctx, tx, tenantID, req.Reason)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit suspend transaction: %w", err)
	}

	item, err := s.GetInstitution(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if paused {
		s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
			TenantID: tenantID, TenantName: item.Name, ActorID: adminID,
			ActionCode: "institution.suspend", ActionName: "暂停服务",
			ObjectType: "institution", ObjectID: strconv.FormatInt(tenantID, 10), ObjectName: item.Name,
			Before: map[string]any{"status": InstitutionStatusActive, "pausedAt": ""},
			After:  map[string]any{"status": InstitutionStatusPaused, "reason": req.Reason},
		})
	}
	return item, nil
}

// ResumeInstitution 恢复服务。重复恢复幂等返回当前档案。
func (s *InstitutionService) ResumeInstitution(ctx context.Context, tenantID int64, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if base.PausedAt == nil {
		// 重复恢复：不报错、不改数据。
		return s.GetInstitution(ctx, tenantID)
	}
	pausedReason := base.PausedReason

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin resume transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	resumed, err := s.store.ResumeTenantTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit resume transaction: %w", err)
	}

	item, err := s.GetInstitution(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if resumed {
		s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
			TenantID: tenantID, TenantName: item.Name, ActorID: adminID,
			ActionCode: "institution.resume", ActionName: "恢复服务",
			ObjectType: "institution", ObjectID: strconv.FormatInt(tenantID, 10), ObjectName: item.Name,
			Before: map[string]any{"status": InstitutionStatusPaused, "reason": pausedReason},
			After:  map[string]any{"status": item.Status},
		})
	}
	return item, nil
}

// ListVoidableOrders 列出该机构可作废的订单（未收款、未核销）。
func (s *InstitutionService) ListVoidableOrders(ctx context.Context, tenantID, adminID int64) ([]Order, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	return s.store.ListVoidableOrders(ctx, tenantID, 100)
}

// VoidOrder 作废订单：未收款（paid_amount=0）且未核销（activated_at 为空）才允许。
// 已作废的订单再次作废幂等返回 200，不改数据、不重复写日志。
func (s *InstitutionService) VoidOrder(ctx context.Context, orderID int64, req VoidOrderRequest, adminID int64) (Order, error) {
	order, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		return Order{}, err
	}
	if err := s.requireTenantAccess(ctx, order.TenantID, adminID); err != nil {
		return Order{}, err
	}
	req, err = req.normalize()
	if err != nil {
		return Order{}, err
	}
	if normalizeEnum(order.Status) == OrderStatusVoid {
		return order, nil
	}
	if err := voidableOrderGuard(order); err != nil {
		return Order{}, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("begin void order transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 锁订单行后重新校验，避免「查完可作废 → 中间被收款核销」的竞态。
	locked, err := s.store.LockOrderTx(ctx, tx, orderID)
	if err != nil {
		return Order{}, err
	}
	if normalizeEnum(locked.Status) == OrderStatusVoid {
		if err := tx.Commit(ctx); err != nil {
			return Order{}, fmt.Errorf("commit idempotent void transaction: %w", err)
		}
		return locked, nil
	}
	if err := voidableOrderGuard(locked); err != nil {
		return Order{}, err
	}
	voided, err := s.store.VoidOrderTx(ctx, tx, orderID)
	if err != nil {
		if errors.Is(err, ErrOrderNotVoidable) {
			return Order{}, &ConflictError{Sentinel: ErrOrderNotVoidable, Message: "订单当前状态不允许作废"}
		}
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, fmt.Errorf("commit void order transaction: %w", err)
	}

	tenantName := ""
	if base, baseErr := s.store.GetInstitutionBase(ctx, order.TenantID); baseErr == nil {
		tenantName = base.Name
	}
	s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
		TenantID: order.TenantID, TenantName: tenantName, ActorID: adminID,
		ActionCode: "order.void", ActionName: "作废订单", LogType: "billing",
		ObjectType: "billing_order", ObjectID: strconv.FormatInt(voided.ID, 10), ObjectName: voided.OrderNo,
		Before: map[string]any{"status": order.Status, "paidAmount": order.PaidAmount.String()},
		After:  map[string]any{"status": OrderStatusVoid, "reason": req.Reason},
	})
	return voided, nil
}

// voidableOrderGuard 校验订单是否允许作废，返回带业务提示的 409。
func voidableOrderGuard(order Order) error {
	if paidCents, err := parseAmountCents(order.PaidAmount); err == nil && paidCents > 0 {
		return &ConflictError{
			Sentinel: ErrOrderNotVoidable,
			Message:  "订单已收款，不能作废；请先完成退款流程",
		}
	}
	if order.ActivatedAt != nil {
		return &ConflictError{Sentinel: ErrOrderNotVoidable, Message: "订单已核销/已开通，不能作废"}
	}
	switch normalizeEnum(order.Status) {
	case OrderStatusCreated:
		return nil
	case OrderStatusVoid:
		return nil
	case OrderStatusCancelled:
		return &ConflictError{Sentinel: ErrOrderNotVoidable, Message: "订单已取消，不能重复作废"}
	case OrderStatusRefunded:
		return &ConflictError{Sentinel: ErrOrderNotVoidable, Message: "订单已退款，不能作废"}
	default:
		return &ConflictError{Sentinel: ErrOrderNotVoidable, Message: "订单当前状态不允许作废"}
	}
}

// CorrectEntitlement 人工调额度：正数加、负数减，改 granted 与 balance（used 不动）。
//
// 口径说明：
//   - 以 product_entitlements.balance 作为可调余额，减少后不能为负；
//   - 正向记 type=grant、负向记 type=usage，source 统一 correction；
//   - 调整绝对值达到 1 个增量包时 approver 必填，审批人姓名写进流水 reason。
func (s *InstitutionService) CorrectEntitlement(ctx context.Context, tenantID int64, req EntitlementCorrectionRequest, adminID int64) (*Institution, error) {
	if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
		return nil, err
	}
	base, err := s.store.GetInstitutionBase(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	req, err = req.normalize()
	if err != nil {
		return nil, err
	}
	index, _, err := s.loadCatalogProducts(ctx)
	if err != nil {
		return nil, err
	}
	product, ok := index[req.ProductCode]
	if !ok {
		return nil, invalidf("产品 %s 不在价目表中", req.ProductCode)
	}
	amountCents, err := parseEntitlementCents("amount", req.Amount)
	if err != nil {
		return nil, invalidf("调整数量 %s", err)
	}
	packCents, needsApproval := correctionNeedsApproval(product, req.Unit, amountCents)
	if needsApproval && req.Approver == "" {
		return nil, invalidf("本次调整达到 1 个增量包（%s %s），请填审批人",
			centsToDecimal(packCents), req.Unit)
	}
	operator := s.service.resolveOperator(ctx, adminID)
	reason := correctionReasonWithApprover(req.Reason, req.Approver)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin entitlement correction transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	// 幂等键命中时直接返回当前机构，不再改额度、不重复写流水与日志。
	// 同租户的调额度都先锁 tenants 行，因此这里的检查与后续写入不会被并发穿透。
	if duplicated, err := s.store.HasCorrectionLedgerTx(ctx, tx, tenantID, req.IdempotencyKey); err != nil {
		return nil, err
	} else if duplicated {
		return s.GetInstitution(ctx, tenantID)
	}
	rows, err := s.store.LoadEntitlementRowsForUpdateTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	row, err := pickCorrectionEntitlement(rows, req.ProductCode, req.Unit, base.AccountMode)
	if err != nil {
		return nil, err
	}
	balanceCents, err := parseEntitlementCents("balance", row.Balance)
	if err != nil {
		return nil, fmt.Errorf("correction %s/%s: %w", row.ProductCode, row.Unit, err)
	}
	grantedCents, err := parseEntitlementCents("granted", row.Granted)
	if err != nil {
		return nil, fmt.Errorf("correction %s/%s: %w", row.ProductCode, row.Unit, err)
	}
	if amountCents < 0 {
		if balanceCents+amountCents < 0 {
			return nil, invalidf("减少后余额不能为负数（当前余额 %s，本次减少 %s）",
				centsToDecimal(balanceCents), centsToDecimal(-amountCents))
		}
		if grantedCents+amountCents < 0 {
			return nil, invalidf("减少后已发额度不能为负数（当前已发 %s）", centsToDecimal(grantedCents))
		}
	}

	newGranted, newBalance, err := s.store.UpdateEntitlementCorrectionTx(ctx, tx, tenantID, row, amountCents)
	if err != nil {
		return nil, err
	}
	if err := s.store.InsertCorrectionLedgerTx(ctx, tx, tenantID, row.ProductCode, row.Unit,
		amountCents, reason, operator, req.IdempotencyKey); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit entitlement correction transaction: %w", err)
	}

	item, err := s.GetInstitution(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	s.recordLifecycleLog(ctx, institutionLifecycleLogInput{
		TenantID: tenantID, TenantName: item.Name, ActorID: adminID,
		ActionCode: "institution.entitlement_correction", ActionName: "人工调额度",
		ObjectType: "institution", ObjectID: strconv.FormatInt(tenantID, 10), ObjectName: item.Name,
		Before: map[string]any{
			"productCode": row.ProductCode, "unit": row.Unit,
			"granted": row.Granted.String(), "balance": row.Balance.String(),
		},
		After: map[string]any{
			"productCode": row.ProductCode, "unit": row.Unit,
			"granted": newGranted.String(), "balance": newBalance.String(),
			"amount": req.Amount.String(), "reason": req.Reason, "approver": req.Approver,
		},
	})
	return item, nil
}

// pickCorrectionEntitlement 选择要调整的额度行：
// 优先与机构当前展示口径一致的 quota_type（试用机构 trial，正式机构 official），
// 同类型内优先 standard 档；都没有时退回到任意一行。
func pickCorrectionEntitlement(rows []Entitlement, productCode, unit, accountMode string) (Entitlement, error) {
	productCode = normalizeEnum(productCode)
	unit = normalizeEnum(unit)
	preferred := QuotaTypeOfficial
	if normalizeEnum(accountMode) == AccountModeTrial {
		preferred = QuotaTypeTrial
	}
	candidates := make([]Entitlement, 0, 4)
	for _, row := range rows {
		if normalizeEnum(row.ProductCode) == productCode && normalizeEnum(row.Unit) == unit {
			candidates = append(candidates, row)
		}
	}
	if len(candidates) == 0 {
		return Entitlement{}, &ConflictError{
			Sentinel: ErrProductNotActive,
			Message:  "该机构还没有这个产品/单位的额度，请先开通或加产品",
		}
	}
	for _, row := range candidates {
		if normalizeEnum(row.QuotaType) == preferred && normalizeEnum(row.Edition) == EditionStandard {
			return row, nil
		}
	}
	for _, row := range candidates {
		if normalizeEnum(row.QuotaType) == preferred {
			return row, nil
		}
	}
	return candidates[0], nil
}

// institutionLifecycleLogInput 是暂停/恢复/作废/调额度的审计日志入参。
type institutionLifecycleLogInput struct {
	TenantID   int64
	TenantName string
	ActorID    int64
	LogType    string
	ActionCode string
	ActionName string
	ObjectType string
	ObjectID   string
	ObjectName string
	Before     map[string]any
	After      map[string]any
}

// recordLifecycleLog 写 system_action_logs；失败只告警，不改变业务结果。
func (s *InstitutionService) recordLifecycleLog(ctx context.Context, input institutionLifecycleLogInput) {
	actorName, _ := s.store.GetOperationsAdminName(ctx, input.ActorID)
	logType := strings.TrimSpace(input.LogType)
	if logType == "" {
		logType = "institution"
	}
	err := s.store.InsertSystemActionLog(ctx, institutionActionLog{
		TenantID:   input.TenantID,
		TenantName: input.TenantName,
		ActorID:    input.ActorID,
		ActorName:  actorName,
		LogType:    logType,
		ActionCode: input.ActionCode,
		ActionName: input.ActionName,
		ObjectType: input.ObjectType,
		ObjectID:   input.ObjectID,
		ObjectName: input.ObjectName,
		Before:     input.Before,
		After:      input.After,
	})
	if err != nil {
		slog.Warn("record billing lifecycle log failed",
			"tenant_id", input.TenantID, "actor_id", input.ActorID, "action", input.ActionCode, "error", err)
	}
}

// valueOrEmpty 取可空字符串指针的值（写日志用）。
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
