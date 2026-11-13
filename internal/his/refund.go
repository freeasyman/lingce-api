package his

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 按项退费（新模型）：
//   - his_refunds 是一次退费单头（金额、原因、操作人），his_refund_items 记录退到哪些收费行；
//   - his_order_items.refunded_quantity 累计已退数量，可退数量 = quantity - refunded_quantity；
//   - his_orders.refunded_total 累计退款，paid_total 保持历史实收口径（净实收 = paid - refunded）；
//   - 只记账，不接支付通道；不自动改已发药/已采样/已执行的业务状态，未执行的检验/检查置 cancelled。

const refundEpsilon = 0.000001

// refundOrderLine 退款计算所需的收费行快照。
type refundOrderLine struct {
	ID               int64
	ItemType         string
	RefID            *int64
	PrescriptionID   *int64
	Name             string
	Quantity         float64
	Payable          float64
	RefundedQuantity float64
	RefundedAmount   float64
}

// refundTarget 本次要退的行与数量。
type refundTarget struct {
	Line     refundOrderLine
	Quantity float64
}

// refundLineAmount 计算单行本次退费金额：按行 payable 的数量比例，四舍五入 2 位。
// 退完剩余数量时用「payable - 已退金额」收口，避免分次退费的四舍五入误差导致累计不等于 payable。
func refundLineAmount(payable, quantity, refundedQuantity, refundedAmount, refundQuantity float64) (float64, error) {
	if quantity <= 0 || refundQuantity <= 0 || refundedQuantity < 0 || refundedAmount < 0 {
		return 0, ErrInvalidRequest
	}
	remaining := quantity - refundedQuantity
	if remaining <= refundEpsilon {
		return 0, ErrRefundQuantityExceeded
	}
	if refundQuantity > remaining+refundEpsilon {
		return 0, ErrRefundQuantityExceeded
	}
	if refundQuantity >= remaining-refundEpsilon {
		amount := roundMoney(payable - refundedAmount)
		if amount < 0 {
			amount = 0
		}
		return amount, nil
	}
	amount := roundMoney(payable * refundQuantity / quantity)
	if amount < 0 {
		return 0, ErrInvalidRequest
	}
	return amount, nil
}

// buildRefundTargets 校验并生成退费目标：不传 items = 整单剩余全退；传了则逐行校验归属、数量。
func buildRefundTargets(lines []refundOrderLine, inputs []RefundItemInput) ([]refundTarget, error) {
	byID := make(map[int64]refundOrderLine, len(lines))
	for _, line := range lines {
		byID[line.ID] = line
	}

	if len(inputs) == 0 {
		targets := make([]refundTarget, 0, len(lines))
		for _, line := range lines {
			remaining := line.Quantity - line.RefundedQuantity
			if remaining <= refundEpsilon {
				continue
			}
			targets = append(targets, refundTarget{Line: line, Quantity: remaining})
		}
		if len(targets) == 0 {
			return nil, ErrRefundQuantityExceeded
		}
		return targets, nil
	}

	seen := make(map[int64]struct{}, len(inputs))
	targets := make([]refundTarget, 0, len(inputs))
	for _, input := range inputs {
		if input.OrderItemID <= 0 || input.Quantity <= 0 {
			return nil, ErrInvalidRequest
		}
		if _, duplicate := seen[input.OrderItemID]; duplicate {
			return nil, ErrInvalidRequest
		}
		seen[input.OrderItemID] = struct{}{}
		line, ok := byID[input.OrderItemID]
		if !ok {
			return nil, ErrInvalidRequest
		}
		remaining := line.Quantity - line.RefundedQuantity
		if input.Quantity > remaining+refundEpsilon {
			return nil, ErrRefundQuantityExceeded
		}
		if remaining <= refundEpsilon {
			return nil, ErrRefundQuantityExceeded
		}
		targets = append(targets, refundTarget{Line: line, Quantity: input.Quantity})
	}
	if len(targets) == 0 {
		return nil, ErrInvalidRequest
	}
	return targets, nil
}

// loadRefundOrderLinesTx 锁定并读取收费行（含已退数量与已退金额）。
func (s *Store) loadRefundOrderLinesTx(ctx context.Context, tx pgx.Tx, orderID int64) ([]refundOrderLine, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, item_type, ref_id, prescription_id, name, quantity, payable, refunded_quantity
		FROM his_order_items WHERE order_id = $1 ORDER BY id FOR UPDATE`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lines := make([]refundOrderLine, 0)
	for rows.Next() {
		var line refundOrderLine
		if err := rows.Scan(&line.ID, &line.ItemType, &line.RefID, &line.PrescriptionID, &line.Name,
			&line.Quantity, &line.Payable, &line.RefundedQuantity); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	amounts := map[int64]float64{}
	rows2, err := tx.Query(ctx, `
		SELECT order_item_id, COALESCE(SUM(amount), 0) FROM his_refund_items
		WHERE order_item_id IN (SELECT id FROM his_order_items WHERE order_id = $1)
		GROUP BY order_item_id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var itemID int64
		var amount float64
		if err := rows2.Scan(&itemID, &amount); err != nil {
			return nil, err
		}
		amounts[itemID] = amount
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}
	for index := range lines {
		lines[index].RefundedAmount = amounts[lines[index].ID]
	}
	return lines, nil
}

// refundRestrictionMaps 业务状态快照：已发药/已采样/已执行的项目禁止退费。
type refundRestrictions struct {
	prescriptionStatus map[int64]string
	labStatus          map[int64][]string
	examStatus         map[int64][]string
	treatmentCompleted map[int64]int
}

func loadRefundRestrictionsTx(ctx context.Context, tx pgx.Tx, tenantID, orderID int64) (refundRestrictions, error) {
	restrictions := refundRestrictions{
		prescriptionStatus: map[int64]string{},
		labStatus:          map[int64][]string{},
		examStatus:         map[int64][]string{},
		treatmentCompleted: map[int64]int{},
	}

	rows, err := tx.Query(ctx, `
		SELECT p.id, p.status FROM his_prescriptions p
		WHERE p.tenant_id = $1 AND p.id IN (
			SELECT prescription_id FROM his_order_items
			WHERE order_id = $2 AND prescription_id IS NOT NULL
		)`, tenantID, orderID)
	if err != nil {
		return restrictions, err
	}
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return restrictions, err
		}
		restrictions.prescriptionStatus[id] = status
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return restrictions, err
	}

	rows, err = tx.Query(ctx, `
		SELECT charge_item_id, status FROM his_lab_requests
		WHERE tenant_id = $1 AND order_id = $2 AND charge_item_id IS NOT NULL`, tenantID, orderID)
	if err != nil {
		return restrictions, err
	}
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return restrictions, err
		}
		restrictions.labStatus[id] = append(restrictions.labStatus[id], status)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return restrictions, err
	}

	rows, err = tx.Query(ctx, `
		SELECT charge_item_id, status FROM his_exam_requests
		WHERE tenant_id = $1 AND order_id = $2 AND charge_item_id IS NOT NULL`, tenantID, orderID)
	if err != nil {
		return restrictions, err
	}
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return restrictions, err
		}
		restrictions.examStatus[id] = append(restrictions.examStatus[id], status)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return restrictions, err
	}

	rows, err = tx.Query(ctx, `
		SELECT charge_item_id, completed_times FROM his_treatment_orders
		WHERE tenant_id = $1 AND order_id = $2 AND charge_item_id IS NOT NULL`, tenantID, orderID)
	if err != nil {
		return restrictions, err
	}
	for rows.Next() {
		var id int64
		var completed int
		if err := rows.Scan(&id, &completed); err != nil {
			rows.Close()
			return restrictions, err
		}
		if completed > restrictions.treatmentCompleted[id] {
			restrictions.treatmentCompleted[id] = completed
		}
	}
	rows.Close()
	return restrictions, rows.Err()
}

// checkRefundLineAllowed 按行类型执行业务校验（药品/检验/检查/治疗/挂号费）。
func checkRefundLineAllowed(line refundOrderLine, restrictions refundRestrictions) error {
	switch line.ItemType {
	case orderItemDrug:
		if line.PrescriptionID != nil {
			if restrictions.prescriptionStatus[*line.PrescriptionID] == "dispensed" {
				return ErrDispensedCannotRefund
			}
		}
	case orderItemRegistrationFee:
		// 挂号费允许退（只记账，不联动挂号状态）。
	case orderItemChargeItem, orderItemBundle:
		if line.RefID == nil {
			return nil
		}
		for _, status := range restrictions.labStatus[*line.RefID] {
			if status == "sampled" || status == "reported" {
				return ErrLabSampledCannotRefund
			}
		}
		for _, status := range restrictions.examStatus[*line.RefID] {
			if status == "in_progress" || status == "reported" {
				return ErrExamStartedCannotRefund
			}
		}
		if restrictions.treatmentCompleted[*line.RefID] > 0 {
			return ErrTreatmentExecutedCannotRefund
		}
	}
	return nil
}

// RefundOrderItems 按项退费：items 为空 = 整单剩余全退（兼容旧整单退接口）。
// 事务内锁单、写退费单头/明细、累加行已退数量与单据累计退款；
// 全部行退完 → status=refunded，否则保持 paid（未退行仍在单上，便于重收）。
func (s *Store) RefundOrderItems(ctx context.Context, tenantID, operatorID, orderID int64, reason string, inputs []RefundItemInput) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_orders WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
		tenantID, orderID).Scan(&status)
	if err == pgx.ErrNoRows {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	// 仅已收费（paid）可退：partial 不允许，pending 走作废。
	if status != "paid" {
		return Order{}, ErrRefundOnlyPaid
	}

	lines, err := s.loadRefundOrderLinesTx(ctx, tx, orderID)
	if err != nil {
		return Order{}, err
	}
	targets, err := buildRefundTargets(lines, inputs)
	if err != nil {
		return Order{}, err
	}
	restrictions, err := loadRefundRestrictionsTx(ctx, tx, tenantID, orderID)
	if err != nil {
		return Order{}, err
	}
	for _, target := range targets {
		if err := checkRefundLineAllowed(target.Line, restrictions); err != nil {
			return Order{}, err
		}
	}

	total := 0.0
	amounts := make([]float64, len(targets))
	for index, target := range targets {
		amount, err := refundLineAmount(target.Line.Payable, target.Line.Quantity,
			target.Line.RefundedQuantity, target.Line.RefundedAmount, target.Quantity)
		if err != nil {
			return Order{}, err
		}
		amounts[index] = amount
		total = roundMoney(total + amount)
	}

	var refundID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO his_refunds (tenant_id, order_id, amount, reason, operator_id)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		tenantID, orderID, total, reason, operatorID).Scan(&refundID); err != nil {
		return Order{}, err
	}

	for index, target := range targets {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_refund_items (refund_id, order_item_id, quantity, amount)
			VALUES ($1, $2, $3, $4)`,
			refundID, target.Line.ID, target.Quantity, amounts[index]); err != nil {
			return Order{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE his_order_items SET refunded_quantity = refunded_quantity + $2
			WHERE id = $1`, target.Line.ID, target.Quantity); err != nil {
			return Order{}, err
		}
	}

	var remaining int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_order_items
		WHERE order_id = $1 AND refunded_quantity < quantity`, orderID).Scan(&remaining); err != nil {
		return Order{}, err
	}
	nextStatus := "paid"
	if remaining == 0 {
		nextStatus = "refunded"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_orders
		SET refunded_total = refunded_total + $2, status = $3, updated_at = NOW()
		WHERE id = $1`, orderID, total, nextStatus); err != nil {
		return Order{}, err
	}

	// 未执行的检验/检查在退款时置 cancelled，避免继续执行；已采样/已执行在上面已拒绝。
	for _, target := range targets {
		if target.Line.RefID == nil {
			continue
		}
		switch target.Line.ItemType {
		case orderItemChargeItem, orderItemBundle:
			if _, err := tx.Exec(ctx, `
				UPDATE his_lab_requests SET status = 'cancelled', updated_at = NOW()
				WHERE tenant_id = $1 AND order_id = $2 AND charge_item_id = $3 AND status = 'pending'`,
				tenantID, orderID, *target.Line.RefID); err != nil {
				return Order{}, err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE his_exam_requests SET status = 'cancelled', updated_at = NOW()
				WHERE tenant_id = $1 AND order_id = $2 AND charge_item_id = $3 AND status = 'pending'`,
				tenantID, orderID, *target.Line.RefID); err != nil {
				return Order{}, err
			}
		}
	}

	// 处方 charged → issued 回冲：仅当该处方在本单的药品行全部退完，避免部分退费后处方被提前回冲。
	prescriptions := map[int64]struct{}{}
	for _, target := range targets {
		if target.Line.PrescriptionID != nil {
			prescriptions[*target.Line.PrescriptionID] = struct{}{}
		}
	}
	for prescriptionID := range prescriptions {
		var unrefunded int
		if err := tx.QueryRow(ctx, `
			SELECT COUNT(*) FROM his_order_items
			WHERE order_id = $1 AND prescription_id = $2 AND refunded_quantity < quantity`,
			orderID, prescriptionID).Scan(&unrefunded); err != nil {
			return Order{}, err
		}
		if unrefunded > 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE his_prescriptions SET status = 'issued', updated_at = NOW()
			WHERE tenant_id = $1 AND id = $2 AND status = 'charged'`,
			tenantID, prescriptionID); err != nil {
			return Order{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, orderID)
}

// RefundOrder 旧整单退费接口（兼容）：内部复用按项退费，items 为空即全部行。
func (s *Store) RefundOrder(ctx context.Context, tenantID, operatorID, orderID int64, reason string) (Order, error) {
	return s.RefundOrderItems(ctx, tenantID, operatorID, orderID, reason, nil)
}
