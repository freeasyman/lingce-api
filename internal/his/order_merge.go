package his

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 收费单合并：同一 encounter 未结清（pending/partial）时复用同一张单，避免一次就诊多张单、多次收费。
//
// 规则：
//   - 开检验/检查：有未结清单则追加收费行（保留原行），否则新建；
//   - 收费台划价：有未结清单则把本次表单行合并进来后重算应收，否则新建；
//   - 合并按 item_type + ref_id + prescription_id 去重，已有行保留下单时的价格快照。

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
// 已有行的价格/数量快照不覆盖（避免字典改价后重算历史行）。
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

// orderRowsTotal 行合计（his_order_items.line_total 已是扣除行优惠后的金额）。
func orderRowsTotal(rows []orderItemRow) float64 {
	total := 0.0
	for _, row := range rows {
		total += row.LineTotal
	}
	return total
}

// orderMergePlan 合并结果：需要新增的行 + 合并后的项目合计/应收。
type orderMergePlan struct {
	InsertRows   []orderItemRow
	ItemsTotal   float64
	PayableTotal float64
}

// planOrderMerge 合并已有行与本次行并重算金额（纯逻辑，便于单测）。
// existing 为空即「无未结清单」路径：全部入参行都新增。
func planOrderMerge(existing, incoming []orderItemRow, discountTotal float64) (orderMergePlan, error) {
	insertRows := mergeOrderItemRows(existing, incoming)
	itemsTotal := orderRowsTotal(existing) + orderRowsTotal(insertRows)
	payable, err := orderPayable(itemsTotal, discountTotal)
	if err != nil {
		return orderMergePlan{}, err
	}
	return orderMergePlan{
		InsertRows:   insertRows,
		ItemsTotal:   itemsTotal,
		PayableTotal: payable,
	}, nil
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

// loadOrderItemRowsTx 读取已落库的收费行（保留原始价格快照）。
func (s *Store) loadOrderItemRowsTx(ctx context.Context, q querier, orderID int64) ([]orderItemRow, error) {
	rows, err := q.Query(ctx, `
		SELECT item_type, ref_id, prescription_id, name, unit_price, quantity, discount_amount, line_total
		FROM his_order_items WHERE order_id = $1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orderItemRow, 0)
	for rows.Next() {
		var row orderItemRow
		if err := rows.Scan(&row.ItemType, &row.RefID, &row.PrescriptionID, &row.Name, &row.UnitPrice,
			&row.Quantity, &row.DiscountAmount, &row.LineTotal); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}

// insertOrderItemRowsTx 落库新增收费行（line_total 与 payable 均为行优惠后金额，与划价一致）。
func insertOrderItemRowsTx(ctx context.Context, tx pgx.Tx, orderID int64, rows []orderItemRow) error {
	for _, row := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_order_items (order_id, item_type, ref_id, prescription_id, name, unit_price,
				quantity, line_total, discount_amount, payable)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $8)`,
			orderID, row.ItemType, row.RefID, row.PrescriptionID, row.Name, row.UnitPrice,
			row.Quantity, row.LineTotal, row.DiscountAmount); err != nil {
			return err
		}
	}
	return nil
}

// mergeOpenOrderItemsTx 把本次表单行合并进未结清单：已有行保留，新行去重后追加。
func (s *Store) mergeOpenOrderItemsTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, encounterID, orderID int64,
	inputs []OrderLineInput,
	discountTotal float64,
) (orderMergePlan, error) {
	existing, err := s.loadOrderItemRowsTx(ctx, tx, orderID)
	if err != nil {
		return orderMergePlan{}, err
	}
	incoming, err := buildOrderItemRows(inputs, func(input OrderLineInput) (resolvedLine, error) {
		return s.resolveOrderLine(ctx, tx, tenantID, encounterID, input)
	})
	if err != nil {
		return orderMergePlan{}, err
	}
	plan, err := planOrderMerge(existing, incoming, discountTotal)
	if err != nil {
		return orderMergePlan{}, err
	}
	if err := insertOrderItemRowsTx(ctx, tx, orderID, plan.InsertRows); err != nil {
		return orderMergePlan{}, err
	}
	return plan, nil
}
