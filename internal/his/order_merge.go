package his

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// 收费单合并：同一 encounter 未结清（pending/partial）时复用同一张单，避免一次就诊多张单、多次收费。
//
// 规则：
//   - 开检验/检查：有未结清单则追加收费行（保留原行），否则新建；
//   - 收费台划价：有未结清单则把本次表单行合并进来后重算应收，否则新建；
//   - 合并按 item_type + ref_id + prescription_id 去重，已有行保留下单时的价格/身份折率快照；
//   - 议价为整单覆盖值，每次合并/调整都按整单可折行重新分摊。

// orderItemKey 收费行去重键。挂号费没有 ref_id/prescription_id，天然按单行去重。
type orderItemKey struct {
	ItemType       string
	RefID          int64
	PrescriptionID int64
}

// orderItemRowKey 用已定价行生成去重键。
func orderItemRowKey(row orderItemRow) orderItemKey {
	key := orderItemKey{ItemType: row.ItemType}
	if row.RefID != nil {
		key.RefID = *row.RefID
	}
	if row.PrescriptionID != nil {
		key.PrescriptionID = *row.PrescriptionID
	}
	return key
}

// mergeOrderItemRows 返回本次需要新增的行：跳过已在单内的行，也跳过本次入参内部的重复行。
// 已有行的价格/数量/身份折率快照不覆盖（避免字典改价后重算历史行）。
func mergeOrderItemRows(existing, incoming []orderItemRow) []orderItemRow {
	seen := make(map[orderItemKey]struct{}, len(existing)+len(incoming))
	for _, row := range existing {
		seen[orderItemRowKey(row)] = struct{}{}
	}
	insertRows := make([]orderItemRow, 0, len(incoming))
	for _, row := range incoming {
		key := orderItemRowKey(row)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		insertRows = append(insertRows, row)
	}
	return insertRows
}

// orderItemRowOriginal 原价：优先用已落库快照，其次单价×数量，最后回退折后+折扣。
func orderItemRowOriginal(row orderItemRow) float64 {
	if row.OriginalTotal > 0 {
		return row.OriginalTotal
	}
	if row.UnitPrice > 0 && row.Quantity > 0 {
		return discountMoney(row.UnitPrice * row.Quantity)
	}
	return discountMoney(row.LineTotal + row.DiscountAmount)
}

// orderRowsTotal 行合计（原价合计，折扣未扣）。
func orderRowsTotal(rows []orderItemRow) float64 {
	total := 0.0
	for index := range rows {
		total += orderItemRowOriginal(rows[index])
	}
	return discountMoney(total)
}

// orderMergePlan 合并结果：需要新增的行 + 全部行（含议价重算结果）+ 合并后金额。
type orderMergePlan struct {
	InsertRows       []orderItemRow
	Rows             []orderItemRow
	ItemsTotal       float64
	IdentityDiscount float64
	NegotiationTotal float64
	PayableTotal     float64
}

// planOrderMergeWithNegotiation 合并已有行与本次行并按整单重新分摊议价。
// existing 为空即「无未结清单」路径：全部入参行都新增。
func planOrderMergeWithNegotiation(existing, incoming []orderItemRow, mode string, value float64) (orderMergePlan, error) {
	if value < 0 {
		return orderMergePlan{}, ErrInvalidRequest
	}
	insertRows := mergeOrderItemRows(existing, incoming)
	rows := make([]orderItemRow, 0, len(existing)+len(insertRows))
	rows = append(rows, existing...)
	rows = append(rows, insertRows...)
	for index := range rows {
		rows[index].OriginalTotal = orderItemRowOriginal(rows[index])
		if rows[index].IdentityRate <= 0 {
			rows[index].IdentityRate = 1
		}
	}
	// 合并/重算时金额议价可能超过新的可折金额，收口到可折金额（与 recalcOrderPricingTx 一致）。
	if mode == negotiationModeAmount && value > 0 {
		base := negotiableBase(rows)
		if value > base {
			value = base
		}
	}
	negotiationTotal, err := applyNegotiation(rows, mode, value)
	if err != nil {
		return orderMergePlan{}, err
	}
	return orderMergePlan{
		InsertRows:       rows[len(existing):],
		Rows:             rows,
		ItemsTotal:       orderRowsTotal(rows),
		IdentityDiscount: orderRowsIdentityDiscount(rows),
		NegotiationTotal: negotiationTotal,
		PayableTotal:     orderRowsPayable(rows),
	}, nil
}

// planOrderMerge 兼容旧签名：第三个参数视为 amount 模式议价金额。
func planOrderMerge(existing, incoming []orderItemRow, discountTotal float64) (orderMergePlan, error) {
	mode := ""
	if discountTotal > 0 {
		mode = negotiationModeAmount
	}
	return planOrderMergeWithNegotiation(existing, incoming, mode, discountTotal)
}

// findOpenOrderTx 读取该 encounter 最近的未结清单并锁行（pending/partial）。
// 先锁 encounter 行，保证同一就诊并发开单时不会各自新建出两张未结清单。
func (s *Store) findOpenOrderTx(ctx context.Context, q querier, tenantID, encounterID int64) (int64, bool, error) {
	var encounter int64
	err := q.QueryRow(ctx, `
		SELECT id FROM encounters WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
		tenantID, encounterID).Scan(&encounter)
	if err == pgx.ErrNoRows {
		return 0, false, ErrNotFound
	}
	if err != nil {
		return 0, false, err
	}

	var orderID int64
	err = q.QueryRow(ctx, `
		SELECT id FROM his_orders
		WHERE tenant_id = $1 AND encounter_id = $2 AND status IN ('pending', 'partial')
		ORDER BY id DESC LIMIT 1
		FOR UPDATE`, tenantID, encounterID).Scan(&orderID)
	if err == pgx.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return orderID, true, nil
}

// loadOrderItemRowsTx 读取已落库的收费行（保留原始价格与身份折率快照）。
func (s *Store) loadOrderItemRowsTx(ctx context.Context, q querier, orderID int64) ([]orderItemRow, error) {
	rows, err := q.Query(ctx, `
		SELECT id, item_type, ref_id, prescription_id, name, category,
		       unit_price, quantity, identity_rate, discount_amount, negotiation_amount, line_total
		FROM his_order_items WHERE order_id = $1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orderItemRow, 0)
	for rows.Next() {
		var row orderItemRow
		if err := rows.Scan(&row.ID, &row.ItemType, &row.RefID, &row.PrescriptionID, &row.Name, &row.Category,
			&row.UnitPrice, &row.Quantity, &row.IdentityRate, &row.DiscountAmount, &row.NegotiationAmount,
			&row.LineTotal); err != nil {
			return nil, err
		}
		row.OriginalTotal = discountMoney(row.UnitPrice * row.Quantity)
		items = append(items, row)
	}
	return items, rows.Err()
}

// insertOrderItemRowsTx 落库新增收费行（payable/line_total 为折后金额）。
func insertOrderItemRowsTx(ctx context.Context, tx pgx.Tx, orderID int64, rows []orderItemRow) error {
	for _, row := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_order_items (order_id, item_type, ref_id, prescription_id, name, category,
				unit_price, quantity, identity_rate, line_total, discount_amount, negotiation_amount, payable)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $10)`,
			orderID, row.ItemType, row.RefID, row.PrescriptionID, row.Name, row.Category,
			row.UnitPrice, row.Quantity, row.IdentityRate, row.LineTotal, row.DiscountAmount,
			row.NegotiationAmount); err != nil {
			return err
		}
	}
	return nil
}

// updateOrderItemAmountsTx 更新已落库行的身份折扣/议价/应收（价格与数量快照不变）。
func updateOrderItemAmountsTx(ctx context.Context, tx pgx.Tx, rows []orderItemRow) error {
	for _, row := range rows {
		if row.ID <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE his_order_items
			SET category = $2, identity_rate = $3, discount_amount = $4,
			    negotiation_amount = $5, line_total = $6, payable = $6
			WHERE id = $1`,
			row.ID, row.Category, row.IdentityRate, row.DiscountAmount,
			row.NegotiationAmount, row.LineTotal); err != nil {
			return err
		}
	}
	return nil
}

// orderNegotiation 收费单上的议价快照。
type orderNegotiation struct {
	Mode   string
	Value  float64
	Reason string
	By     *int64
}

// loadOrderNegotiationTx 读取收费单议价快照。
func loadOrderNegotiationTx(ctx context.Context, q querier, orderID int64) (orderNegotiation, error) {
	var negotiation orderNegotiation
	err := q.QueryRow(ctx, `
		SELECT negotiation_mode, negotiation_value, negotiation_reason, negotiation_by
		FROM his_orders WHERE id = $1`, orderID).
		Scan(&negotiation.Mode, &negotiation.Value, &negotiation.Reason, &negotiation.By)
	return negotiation, err
}

// recalcOrderPricingTx 重新分摊议价并回写行/单金额（事务内）。
// 行身份折率为下单快照，不随身份变更回溯。
func (s *Store) recalcOrderPricingTx(ctx context.Context, tx pgx.Tx, orderID int64) error {
	negotiation, err := loadOrderNegotiationTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	rows, err := s.loadOrderItemRowsTx(ctx, tx, orderID)
	if err != nil {
		return err
	}
	for index := range rows {
		rows[index].OriginalTotal = orderItemRowOriginal(rows[index])
		if rows[index].IdentityRate <= 0 {
			rows[index].IdentityRate = 1
		}
	}
	// 释放/合并导致可折行减少时，按剩余可折行收口金额议价，避免历史议价卡死后续操作。
	if negotiation.Mode == negotiationModeAmount && negotiation.Value > 0 {
		base := 0.0
		for _, row := range rows {
			if isNegotiableCategory(row.Category) {
				base += discountMoney(row.OriginalTotal - row.DiscountAmount)
			}
		}
		if negotiation.Value > base {
			negotiation.Value = discountMoney(base)
			if err := setOrderNegotiationTx(ctx, tx, orderID, negotiation); err != nil {
				return err
			}
		}
	}
	negotiationTotal, err := applyNegotiation(rows, negotiation.Mode, negotiation.Value)
	if err != nil {
		return err
	}
	if err := updateOrderItemAmountsTx(ctx, tx, rows); err != nil {
		return err
	}
	itemsTotal := orderRowsTotal(rows)
	identityDiscount := orderRowsIdentityDiscount(rows)
	discountTotal := discountMoney(identityDiscount + negotiationTotal)
	payable := orderRowsPayable(rows)
	_, err = tx.Exec(ctx, `
		UPDATE his_orders SET items_total = $2, identity_discount_total = $3,
			negotiation_amount = $4, discount_total = $5, payable_total = $6, updated_at = NOW()
		WHERE id = $1`, orderID, itemsTotal, identityDiscount, negotiationTotal, discountTotal, payable)
	return err
}

// setOrderNegotiationTx 覆盖写收费单议价快照。
func setOrderNegotiationTx(ctx context.Context, tx pgx.Tx, orderID int64, negotiation orderNegotiation) error {
	_, err := tx.Exec(ctx, `
		UPDATE his_orders SET negotiation_mode = $2, negotiation_value = $3,
			negotiation_reason = $4, negotiation_by = $5, updated_at = NOW()
		WHERE id = $1`, orderID, negotiation.Mode, negotiation.Value, negotiation.Reason, negotiation.By)
	return err
}

// mergeOpenOrderItemsTx 把本次表单行合并进未结清单：新行去重后追加。
// repriceExisting 为 true 时（pending 单），已有行也按当前身份矩阵重算。
func (s *Store) mergeOpenOrderItemsTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, encounterID, orderID int64,
	inputs []OrderLineInput,
	negotiation orderNegotiation,
	rates map[string]float64,
	repriceExisting bool,
) (orderMergePlan, error) {
	existing, err := s.loadOrderItemRowsTx(ctx, tx, orderID)
	if err != nil {
		return orderMergePlan{}, err
	}
	if repriceExisting {
		applyIdentityPricingToRows(existing, rates)
	}
	incoming, err := buildOrderItemRows(inputs, func(input OrderLineInput) (resolvedLine, error) {
		return s.resolveOrderLine(ctx, tx, tenantID, encounterID, input)
	}, rates)
	if err != nil {
		return orderMergePlan{}, err
	}
	plan, err := planOrderMergeWithNegotiation(existing, incoming, negotiation.Mode, negotiation.Value)
	if err != nil {
		return orderMergePlan{}, err
	}
	if err := insertOrderItemRowsTx(ctx, tx, orderID, plan.InsertRows); err != nil {
		return orderMergePlan{}, err
	}
	// 已有行的议价分摊/应收可能因合并而变化，回写非插入行。
	if err := updateOrderItemAmountsTx(ctx, tx, plan.Rows[:len(existing)]); err != nil {
		return orderMergePlan{}, err
	}
	if err := setOrderNegotiationTx(ctx, tx, orderID, negotiation); err != nil {
		return orderMergePlan{}, err
	}
	return plan, nil
}

// appendOrCreatePendingChargeOrderLinesTx 把若干已定价的挂单行并入该 encounter 的未结清单；
// 没有未结清单时新建 pending 单。治疗项目 + 绑定材料需要一次性并入，故支持多行。
func (s *Store) appendOrCreatePendingChargeOrderLinesTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, createdBy, customerID, encounterID int64,
	lines []orderItemRow,
) (int64, error) {
	if len(lines) == 0 {
		return 0, ErrInvalidRequest
	}

	identity, rates, err := s.discountContextTx(ctx, tx, tenantID, customerID)
	if err != nil {
		return 0, err
	}
	for index := range lines {
		lines[index].Category = discountCategoryForLine(lines[index].ItemType, lines[index].Category)
	}
	applyIdentityPricingToRows(lines, rates)

	orderID, found, err := s.findOpenOrderTx(ctx, tx, tenantID, encounterID)
	if err != nil {
		return 0, err
	}
	if found {
		if err := insertOrderItemRowsTx(ctx, tx, orderID, lines); err != nil {
			return 0, err
		}
		return orderID, s.recalcOrderPricingTx(ctx, tx, orderID)
	}

	seq, err := nextNumber(ctx, tx, tenantID, "order")
	if err != nil {
		return 0, err
	}
	orderNo := fmt.Sprintf("O%s%04d", time.Now().Format("20060102"), seq)
	if err := tx.QueryRow(ctx, `
		INSERT INTO his_orders (tenant_id, customer_id, encounter_id, order_no, status, created_by, price_identity)
		VALUES ($1, $2, $3, $4, 'pending', $5, $6) RETURNING id`,
		tenantID, customerID, encounterID, orderNo, createdBy, identity).Scan(&orderID); err != nil {
		return 0, err
	}
	if err := insertOrderItemRowsTx(ctx, tx, orderID, lines); err != nil {
		return 0, err
	}
	if err := s.recalcOrderPricingTx(ctx, tx, orderID); err != nil {
		return 0, err
	}
	return orderID, nil
}
