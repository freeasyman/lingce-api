package billing

// 收款单与核销的服务层。
//
// 业务核心诉求：一笔收款可以核销多个租户的多个订单（跨租户、可部分）。
// 核销后同事务回写 billing_orders.paid_amount 与状态，保证订单侧数据自洽：
//   0            -> created（未收）
//   0 < paid < 应收 -> paid（部分收）
//   paid >= 应收  -> settled（已收）
// 同一笔收款对同一订单只核销一次：(payment_id, order_id) 唯一，重复提交金额相同按幂等成功返回，
// 金额不同返回 409，不会把 paid_amount 加两次。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// createPaymentInput 是创建收款单的落库入参。
type createPaymentInput struct {
	PaymentNo  string
	Payer      string
	Amount     Decimal
	ReceivedOn time.Time
	Method     string
	VoucherNo  string
	Note       string
	CreatedBy  int64
}

// paymentAllocationInput 是「创建收款单并立即核销到一张订单」的入参（开通收款用）。
type paymentAllocationInput struct {
	TenantID    int64
	Payer       string
	Amount      Decimal
	ReceivedOn  time.Time
	Method      string
	VoucherNo   string
	Note        string
	ActorID     int64
	OrderID     int64
	OrderAmount Decimal
}

// normalizePaymentRequest 校验收款单公共字段。
func (s *InstitutionService) normalizePaymentRequest(payer string, amount Decimal, receivedOn, method, voucherNo, note string) (createPaymentInput, error) {
	amountCents, err := parseAmountCents(amount)
	if err != nil {
		return createPaymentInput{}, invalidf("金额 %s", err)
	}
	if amountCents <= 0 {
		return createPaymentInput{}, invalidf("金额要大于 0")
	}
	received := s.today()
	if strings.TrimSpace(receivedOn) != "" {
		parsed, err := parseDateOnly(receivedOn)
		if err != nil {
			return createPaymentInput{}, invalidf("到账日必须是 YYYY-MM-DD")
		}
		received = parsed
	}
	method = strings.TrimSpace(method)
	if method == "" {
		method = "对公转账"
	}
	payer = strings.TrimSpace(payer)
	note = strings.TrimSpace(note)
	voucherNo = strings.TrimSpace(voucherNo)
	if len([]rune(payer)) > 255 {
		return createPaymentInput{}, invalidf("付款方名称过长")
	}
	if len([]rune(method)) > 64 {
		return createPaymentInput{}, invalidf("收款方式过长")
	}
	if len([]rune(voucherNo)) > 64 {
		return createPaymentInput{}, invalidf("凭证号过长")
	}
	if len([]rune(note)) > 500 {
		return createPaymentInput{}, invalidf("备注过长")
	}
	return createPaymentInput{
		Payer:      payer,
		Amount:     centsToDecimal(amountCents),
		ReceivedOn: received,
		Method:     method,
		VoucherNo:  voucherNo,
		Note:       note,
	}, nil
}

// createPaymentTx 在事务内创建收款单（单号撞号换号重试）。
func (s *InstitutionService) createPaymentTx(ctx context.Context, tx pgx.Tx, input createPaymentInput) (PaymentRecord, error) {
	day := input.ReceivedOn.Format("20060102")
	for attempt := 0; attempt < 5; attempt++ {
		paymentNo, err := s.store.NextPaymentNoTx(ctx, tx, day)
		if err != nil {
			return PaymentRecord{}, err
		}
		input.PaymentNo = paymentNo
		payment, err := s.store.InsertPaymentTx(ctx, tx, input)
		if errors.Is(err, ErrPeerConflict) {
			continue
		}
		if err != nil {
			return PaymentRecord{}, err
		}
		return payment, nil
	}
	return PaymentRecord{}, fmt.Errorf("generate unique payment number: %w", ErrPeerConflict)
}

// createPaymentAndAllocateTx 创建收款单并核销到指定订单。
func (s *InstitutionService) createPaymentAndAllocateTx(ctx context.Context, tx pgx.Tx, input paymentAllocationInput) (PaymentRecord, error) {
	payment, err := s.createPaymentTx(ctx, tx, createPaymentInput{
		Payer:      input.Payer,
		Amount:     input.Amount,
		ReceivedOn: input.ReceivedOn,
		Method:     input.Method,
		VoucherNo:  input.VoucherNo,
		Note:       input.Note,
		CreatedBy:  input.ActorID,
	})
	if err != nil {
		return PaymentRecord{}, err
	}
	order, err := s.store.LockOrderTx(ctx, tx, input.OrderID)
	if err != nil {
		return PaymentRecord{}, err
	}
	amount := input.OrderAmount
	if strings.TrimSpace(string(amount)) == "" {
		amount = input.Amount
	}
	if _, err := s.allocateOrderTx(ctx, tx, payment.ID, order, amount, input.ReceivedOn, input.ActorID); err != nil {
		return PaymentRecord{}, err
	}
	if err := s.refreshPaymentStatusTx(ctx, tx, payment.ID, payment.Amount); err != nil {
		return PaymentRecord{}, err
	}
	refreshed, err := s.store.GetPaymentTx(ctx, tx, payment.ID)
	if err != nil {
		return PaymentRecord{}, err
	}
	return refreshed, nil
}

// allocateOrderTx 把一笔收款核销到一张订单：
//   - 核销金额必须 > 0 且不超过订单未收金额；
//   - (payment, order) 已有核销：金额相同视为幂等（直接返回），不同返回 409；
//   - 成功后回写订单 paid_amount 与状态。
func (s *InstitutionService) allocateOrderTx(ctx context.Context, tx pgx.Tx, paymentID int64, order Order,
	amount Decimal, receivedOn time.Time, actorID int64) (int64, error) {
	amountCents, err := parseAmountCents(amount)
	if err != nil {
		return 0, invalidf("核销金额 %s", err)
	}
	if amountCents <= 0 {
		return 0, invalidf("核销金额要大于 0")
	}
	switch normalizeEnum(order.Status) {
	case OrderStatusCancelled, OrderStatusRefunded, OrderStatusVoid:
		return 0, ErrOrderNotAllocatable
	}
	payableCents, err := parseAmountCents(order.PayableAmount)
	if err != nil {
		return 0, fmt.Errorf("parse order payable: %w", err)
	}
	paidCents, err := parseAmountCents(order.PaidAmount)
	if err != nil {
		return 0, fmt.Errorf("parse order paid amount: %w", err)
	}
	dueCents := payableCents - paidCents
	if dueCents <= 0 {
		return 0, &ValidationError{Message: "该订单已收满，不能继续核销"}
	}
	if amountCents > dueCents {
		return 0, &ValidationError{Message: fmt.Sprintf("核销金额不能超过应收：该订单未收 %s", centsToDecimal(dueCents))}
	}

	existingID, existingAmount, exists, err := s.store.GetExistingAllocationTx(ctx, tx, paymentID, order.ID)
	if err != nil {
		return 0, err
	}
	if exists {
		existingCents, err := parseAmountCents(existingAmount)
		if err == nil && existingCents == amountCents {
			// 重复提交同一笔核销：幂等成功，不再重复回写订单。
			return existingID, nil
		}
		return 0, ErrAllocationConflict
	}

	allocationID, err := s.store.InsertAllocationTx(ctx, tx, paymentID, order.ID, centsToDecimal(amountCents), actorID)
	if err != nil {
		return 0, err
	}

	newPaidCents := paidCents + amountCents
	status := statusAfterPayment(centsToDecimal(newPaidCents), order.PayableAmount)
	var paidAt *time.Time
	if status == OrderStatusSettled {
		value := receivedOn
		paidAt = &value
	}
	if err := s.store.UpdateOrderPaidTx(ctx, tx, order.ID, centsToDecimal(newPaidCents), status, paidAt, actorID); err != nil {
		return 0, err
	}
	return allocationID, nil
}

// refreshPaymentStatusTx 根据核销合计回写收款单状态。
func (s *InstitutionService) refreshPaymentStatusTx(ctx context.Context, tx pgx.Tx, paymentID int64, amount Decimal) error {
	settled, err := s.store.AllocatedTotalTx(ctx, tx, paymentID)
	if err != nil {
		return err
	}
	return s.store.UpdatePaymentStatusTx(ctx, tx, paymentID, derivePaymentStatus(settled, amount))
}

// ReceiveInstitutionPayment 机构维度收款：一笔钱按「最早应收优先」自动核销该机构的未收订单。
// 未核销部分保留在收款单上（status=partial/unallocated），财务可后续跨租户再核销。
func (s *InstitutionService) ReceiveInstitutionPayment(ctx context.Context, tenantID int64, req ReceivePaymentRequest, adminID int64) (*Institution, error) {
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
	amountCents, _ := parseAmountCents(req.Amount)
	receivedOn, _ := parseDateOnly(req.Date)
	payer := firstNonEmpty(base.Legal, base.Name)

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin receive payment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockTenantRow(ctx, tx, tenantID); err != nil {
		return nil, err
	}

	payment, err := s.createPaymentTx(ctx, tx, createPaymentInput{
		Payer:      payer,
		Amount:     centsToDecimal(amountCents),
		ReceivedOn: receivedOn,
		Method:     req.Method,
		Note:       req.Memo,
		CreatedBy:  adminID,
	})
	if err != nil {
		return nil, err
	}

	orders, err := s.store.ListUnpaidOrdersForUpdateTx(ctx, tx, tenantID)
	if err != nil {
		return nil, err
	}
	left := amountCents
	for _, order := range orders {
		if left <= 0 {
			break
		}
		payableCents, err := parseAmountCents(order.PayableAmount)
		if err != nil {
			return nil, fmt.Errorf("parse order payable: %w", err)
		}
		paidCents, err := parseAmountCents(order.PaidAmount)
		if err != nil {
			return nil, fmt.Errorf("parse order paid amount: %w", err)
		}
		dueCents := payableCents - paidCents
		if dueCents <= 0 {
			continue
		}
		take := left
		if take > dueCents {
			take = dueCents
		}
		if _, err := s.allocateOrderTx(ctx, tx, payment.ID, order, centsToDecimal(take), receivedOn, adminID); err != nil {
			return nil, err
		}
		left -= take
	}
	if err := s.refreshPaymentStatusTx(ctx, tx, payment.ID, payment.Amount); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit receive payment transaction: %w", err)
	}
	return s.GetInstitution(ctx, tenantID)
}

// CreatePayment 创建收款单并可同时核销多张订单（跨租户、可部分）。
func (s *InstitutionService) CreatePayment(ctx context.Context, req CreatePaymentRequest, adminID int64) (*PaymentCreateResult, error) {
	if len(req.Allocations) > 200 {
		return nil, invalidf("一次最多核销 200 张订单")
	}
	input, err := s.normalizePaymentRequest(req.PayerName, req.Amount, req.ReceivedOn, req.Method, req.VoucherNo, req.Note)
	if err != nil {
		return nil, err
	}
	allocations, err := normalizePaymentAllocations(req.Allocations)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAllocationsAllowed(ctx, input.Amount, allocations, adminID); err != nil {
		return nil, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create payment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	payment, err := s.createPaymentTx(ctx, tx, createPaymentInput{
		Payer:      input.Payer,
		Amount:     input.Amount,
		ReceivedOn: input.ReceivedOn,
		Method:     input.Method,
		VoucherNo:  input.VoucherNo,
		Note:       input.Note,
		CreatedBy:  adminID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.allocatePaymentTx(ctx, tx, payment, allocations, adminID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create payment transaction: %w", err)
	}
	return s.paymentResult(ctx, payment.ID)
}

// AllocatePayment 给一张已存在的收款单追加核销（同一订单重复提交按幂等处理）。
func (s *InstitutionService) AllocatePayment(ctx context.Context, paymentID int64, req CreatePaymentRequest, adminID int64) (*PaymentCreateResult, error) {
	if paymentID <= 0 {
		return nil, ErrPaymentNotFound
	}
	allocations, err := normalizePaymentAllocations(req.Allocations)
	if err != nil {
		return nil, err
	}
	payment, err := s.store.GetPayment(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureAllocationsAllowed(ctx, payment.Amount, allocations, adminID); err != nil {
		return nil, err
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin allocate payment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	locked, err := s.store.LockPaymentTx(ctx, tx, paymentID)
	if err != nil {
		return nil, err
	}
	if normalizeEnum(locked.Status) == PaymentStatusVoid {
		return nil, &ConflictError{Sentinel: ErrPeerConflict, Message: "收款单已作废，不能核销"}
	}
	if err := s.allocatePaymentTx(ctx, tx, locked, allocations, adminID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit allocate payment transaction: %w", err)
	}
	return s.paymentResult(ctx, paymentID)
}

// normalizedAllocation 是校验后的核销指令。
type normalizedAllocation struct {
	OrderID int64
	Amount  Decimal
}

// normalizePaymentAllocations 校验核销指令：订单必填且不重复，金额 > 0。
func normalizePaymentAllocations(inputs []PaymentAllocationInput) ([]normalizedAllocation, error) {
	allocations := make([]normalizedAllocation, 0, len(inputs))
	seen := make(map[int64]struct{}, len(inputs))
	for index, raw := range inputs {
		orderID := raw.OrderID
		if orderID == 0 {
			orderID = raw.SnakeOrderID
		}
		if orderID <= 0 {
			return nil, invalidf("allocations[%d].orderId is required", index)
		}
		if _, exists := seen[orderID]; exists {
			return nil, invalidf("allocations[%d].orderId is duplicated", index)
		}
		seen[orderID] = struct{}{}
		cents, err := parseAmountCents(raw.Amount)
		if err != nil {
			return nil, invalidf("allocations[%d].amount %s", index, err)
		}
		if cents <= 0 {
			return nil, invalidf("allocations[%d].amount 要大于 0", index)
		}
		allocations = append(allocations, normalizedAllocation{OrderID: orderID, Amount: centsToDecimal(cents)})
	}
	return allocations, nil
}

// ensureAllocationsAllowed 在写库前做权限与金额上限校验：
//   - 每张订单所属租户必须在该运营账号的数据权限内；
//   - 核销合计不能超过收款金额（否则同一条钱会被重复核销）。
func (s *InstitutionService) ensureAllocationsAllowed(ctx context.Context, paymentAmount Decimal, allocations []normalizedAllocation, adminID int64) error {
	if len(allocations) == 0 {
		return nil
	}
	orderIDs := make([]int64, 0, len(allocations))
	for _, allocation := range allocations {
		orderIDs = append(orderIDs, allocation.OrderID)
	}
	tenantIDs, err := s.store.OrderTenantIDs(ctx, orderIDs)
	if err != nil {
		return err
	}
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return err
	}
	totalCents := int64(0)
	for _, allocation := range allocations {
		tenantID, ok := tenantIDs[allocation.OrderID]
		if !ok || tenantID <= 0 {
			return ErrOrderNotFound
		}
		if !scope.CanSeeAll {
			if scope.OrgID == nil {
				return ErrAdminScopeMissing
			}
			visible, err := s.store.TenantVisibleToOrg(ctx, tenantID, *scope.OrgID)
			if err != nil {
				return err
			}
			if !visible {
				return ErrNoPermission
			}
		}
		cents, _ := parseAmountCents(allocation.Amount)
		totalCents += cents
	}
	paymentCents, err := parseAmountCents(paymentAmount)
	if err != nil {
		return fmt.Errorf("parse payment amount: %w", err)
	}
	if totalCents > paymentCents {
		return &ValidationError{Message: "核销合计不能超过收款金额"}
	}
	return nil
}

// allocatePaymentTx 在事务内逐条核销。
//
// 校验三件事：订单存在且可核销、累计核销不超过收款金额、同一订单重复提交按幂等处理。
func (s *InstitutionService) allocatePaymentTx(ctx context.Context, tx pgx.Tx, payment PaymentRecord, allocations []normalizedAllocation, adminID int64) error {
	receivedOn, err := parseDateOnly(payment.Date)
	if err != nil {
		return fmt.Errorf("payment received_on is invalid: %w", err)
	}
	existingSettled, err := s.store.AllocatedTotalTx(ctx, tx, payment.ID)
	if err != nil {
		return err
	}
	settledCents, _ := parseAmountCents(existingSettled)
	amountCents, _ := parseAmountCents(payment.Amount)
	remaining := amountCents - settledCents
	if remaining < 0 {
		remaining = 0
	}

	for _, allocation := range allocations {
		_, existingAmount, exists, err := s.store.GetExistingAllocationTx(ctx, tx, payment.ID, allocation.OrderID)
		if err != nil {
			return err
		}
		allocationCents, _ := parseAmountCents(allocation.Amount)
		if exists {
			existingCents, err := parseAmountCents(existingAmount)
			if err != nil || existingCents != allocationCents {
				return ErrAllocationConflict
			}
			// 重复提交同一笔核销：幂等跳过，不占用剩余额度。
			continue
		}
		if allocationCents > remaining {
			return &ValidationError{Message: "核销合计不能超过收款金额"}
		}
		order, err := s.store.LockOrderTx(ctx, tx, allocation.OrderID)
		if err != nil {
			return err
		}
		if _, err := s.allocateOrderTx(ctx, tx, payment.ID, order, allocation.Amount, receivedOn, adminID); err != nil {
			return err
		}
		remaining -= allocationCents
	}
	return s.refreshPaymentStatusTx(ctx, tx, payment.ID, payment.Amount)
}

// paymentResult 组装创建/核销后的返回：收款单 + 全部核销明细 + 未核销余额。
func (s *InstitutionService) paymentResult(ctx context.Context, paymentID int64) (*PaymentCreateResult, error) {
	record, err := s.store.GetPaymentRecord(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	allocations, err := s.store.ListPaymentAllocations(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	settled, _ := parseAmountCents(record.Settled)
	amount, _ := parseAmountCents(record.Amount)
	unallocated := amount - settled
	if unallocated < 0 {
		unallocated = 0
	}
	return &PaymentCreateResult{
		Payment:            record,
		Allocations:        allocations,
		UnallocatedBalance: centsToDecimal(unallocated),
	}, nil
}

// ListPayments 返回收款单列表（按运营账号数据权限收敛）。
func (s *InstitutionService) ListPayments(ctx context.Context, adminID int64) (*PaymentListResult, error) {
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return nil, err
	}
	orgID := int64(0)
	if !scope.CanSeeAll {
		if scope.OrgID == nil {
			return nil, ErrAdminScopeMissing
		}
		orgID = *scope.OrgID
	}
	items, err := s.store.ListPaymentRecords(ctx, orgID, adminID)
	if err != nil {
		return nil, err
	}
	return &PaymentListResult{Items: items, IsMock: false}, nil
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
