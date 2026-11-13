package his

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 收费项目「随项目带出」：父项目（治疗/处置）绑定子项目（耗材/材料）。
// 医生开治疗医嘱时，父项目与绑定子项目在同一事务内并入该就诊的未结清单；
// 收费台仍可后续删除材料行（pending 单可改行）。

// normalizeChargeItemLinkInputs 校验整体覆盖写入参（纯逻辑）。
// 父项目必须有效；子项目 id 必须为正、非自身、数量为正、同一子项目不得重复。
func normalizeChargeItemLinkInputs(parentID int64, inputs []ChargeItemLinkInput) ([]ChargeItemLinkInput, error) {
	if parentID <= 0 {
		return nil, ErrInvalidRequest
	}
	seen := make(map[int64]struct{}, len(inputs))
	normalized := make([]ChargeItemLinkInput, 0, len(inputs))
	for _, input := range inputs {
		if input.ChildChargeItemID <= 0 || input.ChildChargeItemID == parentID {
			return nil, ErrInvalidRequest
		}
		if input.Quantity <= 0 {
			return nil, ErrInvalidRequest
		}
		if _, duplicate := seen[input.ChildChargeItemID]; duplicate {
			return nil, ErrInvalidRequest
		}
		seen[input.ChildChargeItemID] = struct{}{}
		normalized = append(normalized, input)
	}
	return normalized, nil
}

// chargeItemLinkColumns 绑定查询列（子项目名称/类别/单位/价格一并返回，管理页子表可直接展示）。
const chargeItemLinkColumns = `l.id, l.parent_charge_item_id, l.child_charge_item_id,
	ci.name, ci.category, ci.unit, ci.price, l.quantity`

// listChargeItemLinks 读取父项目的绑定子项目（调用方负责父项目存在性校验）。
func listChargeItemLinks(ctx context.Context, q querier, tenantID, parentID int64) ([]ChargeItemLink, error) {
	rows, err := q.Query(ctx, `
		SELECT `+chargeItemLinkColumns+`
		FROM his_charge_item_links l
		JOIN his_charge_items ci ON ci.tenant_id = l.tenant_id AND ci.id = l.child_charge_item_id
		WHERE l.tenant_id = $1 AND l.parent_charge_item_id = $2
		ORDER BY ci.name, l.id`, tenantID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	links := make([]ChargeItemLink, 0)
	for rows.Next() {
		var link ChargeItemLink
		if err := rows.Scan(&link.ID, &link.ParentChargeItemID, &link.ChildChargeItemID,
			&link.ChildName, &link.ChildCategory, &link.ChildUnit, &link.ChildPrice, &link.Quantity); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// GetChargeItemLinks 管理页读取父项目的绑定子项目。
func (s *Store) GetChargeItemLinks(ctx context.Context, tenantID, parentID int64) ([]ChargeItemLink, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM his_charge_items WHERE tenant_id = $1 AND id = $2)`,
		tenantID, parentID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	return listChargeItemLinks(ctx, s.pool, tenantID, parentID)
}

// ReplaceChargeItemLinks 整体覆盖写父项目绑定：校验父子均本租户且 active、非自身、数量 > 0。
// 空数组表示清空绑定。
func (s *Store) ReplaceChargeItemLinks(ctx context.Context, tenantID, parentID int64, inputs []ChargeItemLinkInput) ([]ChargeItemLink, error) {
	normalized, err := normalizeChargeItemLinkInputs(parentID, inputs)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var parentName string
	err = tx.QueryRow(ctx, `
		SELECT name FROM his_charge_items
		WHERE tenant_id = $1 AND id = $2 AND status = 'active' FOR UPDATE`,
		tenantID, parentID).Scan(&parentName)
	if err == pgx.ErrNoRows {
		return nil, ErrInvalidRequest
	}
	if err != nil {
		return nil, err
	}

	// 子项目快照：逐个校验本租户 + active（跨租户/停用一律按无效入参处理）。
	type childSnapshot struct {
		ID       int64
		Name     string
		Category string
		Unit     string
		Price    float64
	}
	children := make(map[int64]childSnapshot, len(normalized))
	for _, input := range normalized {
		if _, ok := children[input.ChildChargeItemID]; ok {
			continue
		}
		var child childSnapshot
		err := tx.QueryRow(ctx, `
			SELECT id, name, category, unit, price FROM his_charge_items
			WHERE tenant_id = $1 AND id = $2 AND status = 'active'`,
			tenantID, input.ChildChargeItemID).
			Scan(&child.ID, &child.Name, &child.Category, &child.Unit, &child.Price)
		if err == pgx.ErrNoRows {
			return nil, ErrInvalidRequest
		}
		if err != nil {
			return nil, err
		}
		children[child.ID] = child
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM his_charge_item_links WHERE tenant_id = $1 AND parent_charge_item_id = $2`,
		tenantID, parentID); err != nil {
		return nil, err
	}
	for _, input := range normalized {
		child := children[input.ChildChargeItemID]
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_charge_item_links (
				tenant_id, parent_charge_item_id, child_charge_item_id, quantity
			) VALUES ($1, $2, $3, $4)`,
			tenantID, parentID, child.ID, input.Quantity); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return listChargeItemLinks(ctx, s.pool, tenantID, parentID)
}

// linkedChargeLinesTx 生成父项目绑定子项目的收费行（仅 active 子项目）。
// 价格同样只取收费字典价，不接受前端传价；数量取绑定配置数量。
func (s *Store) linkedChargeLinesTx(ctx context.Context, q querier, tenantID, parentID int64) ([]orderItemRow, error) {
	rows, err := q.Query(ctx, `
		SELECT ci.id, ci.name, ci.price, ci.category, l.quantity
		FROM his_charge_item_links l
		JOIN his_charge_items ci ON ci.tenant_id = l.tenant_id AND ci.id = l.child_charge_item_id
		WHERE l.tenant_id = $1 AND l.parent_charge_item_id = $2 AND ci.status = 'active'
		ORDER BY l.id`, tenantID, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	lines := make([]orderItemRow, 0)
	for rows.Next() {
		var childID int64
		var name, category string
		var price, quantity float64
		if err := rows.Scan(&childID, &name, &price, &category, &quantity); err != nil {
			return nil, err
		}
		lineTotal, err := orderLineAmount(price, quantity, 0)
		if err != nil {
			return nil, err
		}
		refID := childID
		lines = append(lines, orderItemRow{
			ItemType:  orderItemChargeItem,
			RefID:     &refID,
			Name:      name,
			Category:  category,
			UnitPrice: price,
			Quantity:  quantity,
			LineTotal: lineTotal,
		})
	}
	return lines, rows.Err()
}
