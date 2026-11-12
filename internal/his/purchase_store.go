package his

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const purchaseOrderColumns = `o.id, o.order_no, o.supplier, o.status, o.total_amount,
	o.created_by, o.created_at, o.updated_at`

// ListPurchaseOrders 采购单列表（?status=&keyword=），keyword 命中单号/供应商/药品名。
func (s *Store) ListPurchaseOrders(ctx context.Context, tenantID int64, status, keyword string) ([]PurchaseOrder, error) {
	pattern := "%" + keyword + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT `+purchaseOrderColumns+`
		FROM his_purchase_orders o
		WHERE o.tenant_id = $1
		  AND ($2 = '' OR o.status = $2)
		  AND ($3 = '' OR o.order_no ILIKE $4 OR o.supplier ILIKE $4 OR EXISTS (
		        SELECT 1 FROM his_purchase_order_items i
		        WHERE i.order_id = o.id AND i.drug_name ILIKE $4))
		ORDER BY o.created_at DESC, o.id DESC
		LIMIT 200`, tenantID, status, keyword, pattern)
	if err != nil {
		return nil, err
	}

	orders := make([]PurchaseOrder, 0)
	for rows.Next() {
		var order PurchaseOrder
		if err := rows.Scan(&order.ID, &order.OrderNo, &order.Supplier, &order.Status, &order.TotalAmount,
			&order.CreatedBy, &order.CreatedAt, &order.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		orders = append(orders, order)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	pointers := make([]*PurchaseOrder, len(orders))
	for index := range orders {
		pointers[index] = &orders[index]
	}
	if err := s.attachPurchaseItems(ctx, s.pool, pointers); err != nil {
		return nil, err
	}
	return orders, nil
}

// CreatePurchaseOrder 新建采购单：校验租户内药品、生成采购单号、按明细合计。
func (s *Store) CreatePurchaseOrder(ctx context.Context, tenantID, createdBy int64, req CreatePurchaseOrderRequest) (PurchaseOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)

	drugNames := make([]string, len(req.Items))
	for index, item := range req.Items {
		err := tx.QueryRow(ctx, `
			SELECT name FROM his_drugs WHERE tenant_id = $1 AND id = $2`,
			tenantID, item.DrugID).Scan(&drugNames[index])
		if errors.Is(err, pgx.ErrNoRows) {
			return PurchaseOrder{}, ErrInvalidRequest
		}
		if err != nil {
			return PurchaseOrder{}, err
		}
	}

	seq, err := nextNumber(ctx, tx, tenantID, "po")
	if err != nil {
		return PurchaseOrder{}, err
	}
	orderNo := purchaseOrderNo(seq, time.Now())
	total := purchaseOrderTotal(req.Items)

	var orderID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO his_purchase_orders (tenant_id, order_no, supplier, status, total_amount, created_by)
		VALUES ($1, $2, $3, 'draft', $4, $5)
		RETURNING id`,
		tenantID, orderNo, req.Supplier, total, createdBy).Scan(&orderID); err != nil {
		return PurchaseOrder{}, err
	}

	for index, item := range req.Items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_purchase_order_items
				(order_id, drug_id, drug_name, quantity, purchase_price, batch_no, expiry_date, amount)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			orderID, item.DrugID, drugNames[index], item.Quantity, item.PurchasePrice,
			item.BatchNo, item.ExpiryDate, purchaseLineAmount(item.Quantity, item.PurchasePrice)); err != nil {
			return PurchaseOrder{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return PurchaseOrder{}, err
	}
	return s.loadPurchaseOrder(ctx, s.pool, tenantID, orderID)
}

// ReceivePurchaseOrder 采购收货：逐行复用入库逻辑写库存与流水，整单事务提交。
func (s *Store) ReceivePurchaseOrder(ctx context.Context, tenantID, operatorID, orderID int64) (PurchaseOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_purchase_orders
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, orderID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseOrder{}, ErrNotFound
	}
	if err != nil {
		return PurchaseOrder{}, err
	}
	if err := purchaseReceiveStatusError(status); err != nil {
		return PurchaseOrder{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id, drug_id, quantity, purchase_price, batch_no, to_char(expiry_date, 'YYYY-MM-DD')
		FROM his_purchase_order_items
		WHERE order_id = $1
		ORDER BY id`, orderID)
	if err != nil {
		return PurchaseOrder{}, err
	}
	items := make([]PurchaseOrderItem, 0)
	for rows.Next() {
		var item PurchaseOrderItem
		if err := rows.Scan(&item.ID, &item.DrugID, &item.Quantity, &item.PurchasePrice,
			&item.BatchNo, &item.ExpiryDate); err != nil {
			rows.Close()
			return PurchaseOrder{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return PurchaseOrder{}, err
	}
	rows.Close()
	if len(items) == 0 {
		return PurchaseOrder{}, ErrInvalidRequest
	}

	for _, item := range items {
		if item.Quantity <= 0 || item.PurchasePrice < 0 {
			return PurchaseOrder{}, ErrInvalidRequest
		}
		refID := orderID
		err := inboundStockTx(ctx, tx, tenantID, operatorID, InboundStockRequest{
			DrugID:     item.DrugID,
			BatchNo:    item.BatchNo,
			Quantity:   item.Quantity,
			ExpiryDate: item.ExpiryDate,
			Location:   "采购入库",
		}, "purchase_order", &refID)
		if err != nil {
			return PurchaseOrder{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE his_purchase_orders SET status = 'received', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, tenantID, orderID); err != nil {
		return PurchaseOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseOrder{}, err
	}
	return s.loadPurchaseOrder(ctx, s.pool, tenantID, orderID)
}

// CancelPurchaseOrder 取消采购单（仅 draft/ordered）。
func (s *Store) CancelPurchaseOrder(ctx context.Context, tenantID, orderID int64) (PurchaseOrder, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PurchaseOrder{}, err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `
		SELECT status FROM his_purchase_orders
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, orderID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseOrder{}, ErrNotFound
	}
	if err != nil {
		return PurchaseOrder{}, err
	}
	if err := purchaseCancelStatusError(status); err != nil {
		return PurchaseOrder{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_purchase_orders SET status = 'cancelled', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2`, tenantID, orderID); err != nil {
		return PurchaseOrder{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PurchaseOrder{}, err
	}
	return s.loadPurchaseOrder(ctx, s.pool, tenantID, orderID)
}

func (s *Store) loadPurchaseOrder(ctx context.Context, q querier, tenantID, orderID int64) (PurchaseOrder, error) {
	var order PurchaseOrder
	err := q.QueryRow(ctx, `
		SELECT `+purchaseOrderColumns+`
		FROM his_purchase_orders o
		WHERE o.tenant_id = $1 AND o.id = $2`, tenantID, orderID).
		Scan(&order.ID, &order.OrderNo, &order.Supplier, &order.Status, &order.TotalAmount,
			&order.CreatedBy, &order.CreatedAt, &order.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PurchaseOrder{}, ErrNotFound
	}
	if err != nil {
		return PurchaseOrder{}, err
	}
	if err := s.attachPurchaseItems(ctx, q, []*PurchaseOrder{&order}); err != nil {
		return PurchaseOrder{}, err
	}
	return order, nil
}

// attachPurchaseItems 批量补全明细与品项数（租户隔离由订单 ID 来源保证）。
func (s *Store) attachPurchaseItems(ctx context.Context, q querier, orders []*PurchaseOrder) error {
	ids := make([]int64, 0, len(orders))
	byID := make(map[int64]*PurchaseOrder, len(orders))
	for _, order := range orders {
		if order == nil {
			continue
		}
		order.Items = make([]PurchaseOrderItem, 0)
		order.ItemCount = 0
		ids = append(ids, order.ID)
		byID[order.ID] = order
	}
	if len(ids) == 0 {
		return nil
	}

	rows, err := q.Query(ctx, `
		SELECT id, order_id, drug_id, drug_name, quantity, purchase_price, batch_no,
		       to_char(expiry_date, 'YYYY-MM-DD'), amount
		FROM his_purchase_order_items
		WHERE order_id = ANY($1::bigint[])
		ORDER BY id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var item PurchaseOrderItem
		if err := rows.Scan(&item.ID, &item.OrderID, &item.DrugID, &item.DrugName, &item.Quantity,
			&item.PurchasePrice, &item.BatchNo, &item.ExpiryDate, &item.Amount); err != nil {
			return err
		}
		if order, ok := byID[item.OrderID]; ok {
			order.Items = append(order.Items, item)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, order := range orders {
		if order != nil {
			order.ItemCount = len(order.Items)
		}
	}
	return nil
}
