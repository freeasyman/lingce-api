package his

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// 检验/检查开单的收费联动：申请单可选收费项目，并在同一事务内生成 pending 收费单（挂单）。
// 价格只取 his_charge_items.price 快照，不接受前端传价；未匹配到收费项目时允许开单但不建单。

const (
	chargeCategoryLab  = "检验"
	chargeCategoryExam = "检查"

	chargeItemType = "charge_item"
)

// chargeItemSnapshot 收费项目字典快照（下单时的名称与价格，后续改价不影响已挂单）。
type chargeItemSnapshot struct {
	ID       int64
	Name     string
	Price    float64
	Category string
}

// requestChargeLink 开单后回写申请单的收费关联字段。
// 未匹配到收费项目时 ChargeItemID/OrderID 为空、ChargeAmount 为 0。
type requestChargeLink struct {
	ChargeItemID *int64
	ChargeAmount float64
	OrderID      *int64
}

// pendingChargeLine 是一条待生成的收费行（单行挂单：数量 1、无优惠）。
type pendingChargeLine struct {
	ItemType  string
	RefID     int64
	Name      string
	Category  string
	UnitPrice float64
	Quantity  float64
	LineTotal float64
	Payable   float64
}

// normalizeChargeItemName 名称匹配基准：去首尾空白 + 忽略大小写（中文项目名等价于精确匹配）。
func normalizeChargeItemName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// normalizeChargeItemID 校验可选收费项目 id：0 表示未选（允许按名称自动匹配），负数非法。
func normalizeChargeItemID(chargeItemID int64) error {
	if chargeItemID < 0 {
		return ErrInvalidRequest
	}
	return nil
}

// matchChargeItemsByName 在候选收费项目里按名称精确匹配，返回全部命中（用于识别歧义）。
func matchChargeItemsByName(items []chargeItemSnapshot, name string) []chargeItemSnapshot {
	needle := normalizeChargeItemName(name)
	if needle == "" || utf8.RuneCountInString(needle) > maxLabItemNameRunes {
		return nil
	}
	matches := make([]chargeItemSnapshot, 0, 1)
	for _, item := range items {
		if normalizeChargeItemName(item.Name) == needle {
			matches = append(matches, item)
		}
	}
	return matches
}

// findChargeItemByID 在候选收费项目里按 id 精确查找。
func findChargeItemByID(items []chargeItemSnapshot, id int64) (chargeItemSnapshot, bool) {
	if id <= 0 {
		return chargeItemSnapshot{}, false
	}
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return chargeItemSnapshot{}, false
}

// selectChargeItem 决定本次开单使用的收费项目：
//   - chargeItemID > 0：必须命中候选（候选已按租户 + active 过滤），否则 ErrInvalidRequest；
//   - chargeItemID == 0：按项目名精确匹配，未命中返回 matched=false（允许开单、不建收费单）。
func selectChargeItem(items []chargeItemSnapshot, chargeItemID int64, itemName string) (chargeItemSnapshot, bool, error) {
	if err := normalizeChargeItemID(chargeItemID); err != nil {
		return chargeItemSnapshot{}, false, err
	}
	if chargeItemID > 0 {
		item, ok := findChargeItemByID(items, chargeItemID)
		if !ok {
			return chargeItemSnapshot{}, false, ErrInvalidRequest
		}
		return item, true, nil
	}
	// 名称歧义（同名不同价）时不做自动匹配，交给医生在弹窗里显式选择，避免挂错价格。
	matches := matchChargeItemsByName(items, itemName)
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return chargeItemSnapshot{}, false, nil
}

// newPendingChargeLine 用字典价生成挂单收费行（金额快照；价格异常时拒绝开单）。
func newPendingChargeLine(item chargeItemSnapshot) (pendingChargeLine, error) {
	if item.ID <= 0 || item.Price < 0 {
		return pendingChargeLine{}, ErrInvalidRequest
	}
	return pendingChargeLine{
		ItemType:  chargeItemType,
		RefID:     item.ID,
		Name:      item.Name,
		Category:  item.Category,
		UnitPrice: item.Price,
		Quantity:  1,
		LineTotal: item.Price,
		Payable:   item.Price,
	}, nil
}

// activeChargeItemsTx 读取本租户指定分类下的启用收费项目（字典规模很小，便于统一走纯逻辑匹配）。
func (s *Store) activeChargeItemsTx(ctx context.Context, q querier, tenantID int64, category string) ([]chargeItemSnapshot, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, price, category FROM his_charge_items
		WHERE tenant_id = $1 AND status = 'active' AND category = $2
		ORDER BY id`, tenantID, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]chargeItemSnapshot, 0)
	for rows.Next() {
		var item chargeItemSnapshot
		if err := rows.Scan(&item.ID, &item.Name, &item.Price, &item.Category); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// activeChargeItemByIDTx 读取本租户启用的收费项目（跨租户/已停用一律视为无效入参，不泄露存在性）。
func (s *Store) activeChargeItemByIDTx(ctx context.Context, q querier, tenantID, id int64) (chargeItemSnapshot, error) {
	var item chargeItemSnapshot
	err := q.QueryRow(ctx, `
		SELECT id, name, price, category FROM his_charge_items
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'`, tenantID, id).
		Scan(&item.ID, &item.Name, &item.Price, &item.Category)
	if err == pgx.ErrNoRows {
		return chargeItemSnapshot{}, ErrInvalidRequest
	}
	if err != nil {
		return chargeItemSnapshot{}, err
	}
	return item, nil
}

// createPendingChargeOrderTx 生成单行 pending 收费单（挂单），返回收费单 id。
// 保留给单行挂单调用；多行（治疗 + 绑定材料）走 appendOrCreatePendingChargeOrderLinesTx。
func (s *Store) createPendingChargeOrderTx(ctx context.Context, tx pgx.Tx, tenantID, createdBy, customerID, encounterID int64, item chargeItemSnapshot) (int64, error) {
	line, err := newPendingChargeLine(item)
	if err != nil {
		return 0, err
	}
	return s.appendOrCreatePendingChargeOrderLinesTx(ctx, tx, tenantID, createdBy, customerID, encounterID,
		[]orderItemRow{{
			ItemType:  line.ItemType,
			RefID:     &line.RefID,
			Name:      line.Name,
			Category:  line.Category,
			UnitPrice: line.UnitPrice,
			Quantity:  line.Quantity,
			LineTotal: line.LineTotal,
		}})
}

// appendOrCreatePendingChargeOrderTx 检验/检查挂单：该 encounter 已有未结清单则追加行并重算，
// 否则新建一张 pending 单。返回申请单 order_id 指向的收费单 id。
// 追加时保留单内已有行（如上一张检验/检查、挂号费+处方），已有折扣不丢失。
func (s *Store) appendOrCreatePendingChargeOrderTx(ctx context.Context, tx pgx.Tx, tenantID, createdBy, customerID, encounterID int64, item chargeItemSnapshot) (int64, error) {
	line, err := newPendingChargeLine(item)
	if err != nil {
		return 0, err
	}
	return s.appendOrCreatePendingChargeOrderLinesTx(ctx, tx, tenantID, createdBy, customerID, encounterID,
		[]orderItemRow{{
			ItemType:  line.ItemType,
			RefID:     &line.RefID,
			Name:      line.Name,
			Category:  line.Category,
			UnitPrice: line.UnitPrice,
			Quantity:  line.Quantity,
			LineTotal: line.LineTotal,
		}})
}

// attachChargeToLabExamRequestTx 解析收费项目并在同一事务内挂单，返回回写申请单的关联字段。
// explicitChargeItemID > 0 时按 id 校验（租户 + active）；否则按 itemName 在本分类启用项目里精确匹配。
func (s *Store) attachChargeToLabExamRequestTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, createdBy, customerID, encounterID int64,
	explicitChargeItemID int64,
	itemName, category string,
) (requestChargeLink, error) {
	if err := normalizeChargeItemID(explicitChargeItemID); err != nil {
		return requestChargeLink{}, err
	}

	candidates := make([]chargeItemSnapshot, 0)
	if explicitChargeItemID > 0 {
		item, err := s.activeChargeItemByIDTx(ctx, tx, tenantID, explicitChargeItemID)
		if err != nil {
			return requestChargeLink{}, err
		}
		candidates = append(candidates, item)
	} else {
		items, err := s.activeChargeItemsTx(ctx, tx, tenantID, category)
		if err != nil {
			return requestChargeLink{}, err
		}
		candidates = items
	}

	item, matched, err := selectChargeItem(candidates, explicitChargeItemID, itemName)
	if err != nil {
		return requestChargeLink{}, err
	}
	if !matched {
		return requestChargeLink{}, nil
	}

	orderID, err := s.appendOrCreatePendingChargeOrderTx(ctx, tx, tenantID, createdBy, customerID, encounterID, item)
	if err != nil {
		return requestChargeLink{}, err
	}
	chargeItemID := item.ID
	return requestChargeLink{
		ChargeItemID: &chargeItemID,
		ChargeAmount: item.Price,
		OrderID:      &orderID,
	}, nil
}

// releaseLabExamChargeTx 取消检验/检查申请时释放它占用的一条收费行。
// 合并收费后一张单可能承载多个申请/挂号费/处方，不能整单作废：只删本次申请对应的那一行，
// 单内仍有其它行时保留 pending 并重算，删空后才作废整单。
// 已发生收款的单据（partial/paid）不能自动释放，返回 ErrInvalidState 让调用方先走退费。
func (s *Store) releaseLabExamChargeTx(ctx context.Context, tx pgx.Tx, tenantID, orderID, chargeItemID int64) error {
	var status string
	err := tx.QueryRow(ctx, `
		SELECT status FROM his_orders WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, orderID).Scan(&status)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	switch status {
	case "pending":
		tag, err := tx.Exec(ctx, `
			DELETE FROM his_order_items
			WHERE id IN (
				SELECT id FROM his_order_items
				WHERE order_id = $1 AND item_type = $2 AND ref_id = $3
				ORDER BY id LIMIT 1
			)`, orderID, chargeItemType, chargeItemID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		var remaining int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM his_order_items WHERE order_id = $1`, orderID).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			_, err := tx.Exec(ctx, `UPDATE his_orders SET status = 'void', updated_at = NOW() WHERE tenant_id = $1 AND id = $2`, tenantID, orderID)
			return err
		}
		return s.recalcOrderPricingTx(ctx, tx, orderID)
	case "void", "refunded":
		return nil
	default:
		// partial/paid：已发生收款，必须先退费再取消。
		return ErrInvalidState
	}
}
