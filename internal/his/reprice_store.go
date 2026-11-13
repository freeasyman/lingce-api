package his

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// orderRepriceState 一次收费/合并时按当前身份重算后的订单状态。
type orderRepriceState struct {
	Status           string
	PaidTotal        float64
	PreviousPayable  float64
	PayableTotal     float64
	PreviousIdentity string
	CurrentIdentity  string
	Repriced         bool
}

// OrderRepricePreview 收费弹窗打开时的重算预览（不落库、不留痕）。
type OrderRepricePreview struct {
	Repriced              bool    `json:"repriced"`
	PreviousPayable       float64 `json:"previous_payable"`
	PayableTotal          float64 `json:"payable_total"`
	PreviousIdentity      string  `json:"previous_identity"`
	PriceIdentity         string  `json:"price_identity"`
	PaidTotal             float64 `json:"paid_total"`
	ItemsTotal            float64 `json:"items_total"`
	IdentityDiscountTotal float64 `json:"identity_discount_total"`
	NegotiationAmount     float64 `json:"negotiation_amount"`
	DiscountTotal         float64 `json:"discount_total"`
}

// repriceOrderTx 在事务内按患者当前价格身份重算订单（仅 pending）。
// partial 单仅在身份未变时放行（不重算）；paid/void/refunded 不重算。
func (s *Store) repriceOrderTx(ctx context.Context, tx pgx.Tx, tenantID, orderID, operatorID int64) (orderRepriceState, error) {
	var state orderRepriceState
	var customerID int64
	var orderIdentity string
	var payable, paid float64
	err := tx.QueryRow(ctx, `
		SELECT status, customer_id, payable_total, paid_total,
		       COALESCE(NULLIF(price_identity, ''), 'standard')
		FROM his_orders WHERE tenant_id = $1 AND id = $2 FOR UPDATE`,
		tenantID, orderID).Scan(&state.Status, &customerID, &payable, &paid, &orderIdentity)
	if err != nil {
		return state, err
	}
	state.PreviousPayable = payable
	state.PayableTotal = payable
	state.PaidTotal = paid
	state.PreviousIdentity = orderIdentity
	state.CurrentIdentity = orderIdentity

	identity, rates, err := s.discountContextTx(ctx, tx, tenantID, customerID)
	if err != nil {
		return state, err
	}
	state.CurrentIdentity = identity
	if err := checkOrderIdentityChangeAllowed(state.Status, orderIdentity, identity); err != nil {
		return state, err
	}
	if !shouldRepriceOrder(state.Status) {
		return state, nil
	}

	rows, err := s.loadOrderItemRowsTx(ctx, tx, orderID)
	if err != nil {
		return state, err
	}
	negotiation, err := loadOrderNegotiationTx(ctx, tx, orderID)
	if err != nil {
		return state, err
	}
	plan, err := planOrderReprice(rows, negotiation, rates)
	if err != nil {
		return state, err
	}
	if plan.Negotiation.Value != negotiation.Value || plan.Negotiation.Mode != negotiation.Mode {
		if err := setOrderNegotiationTx(ctx, tx, orderID, plan.Negotiation); err != nil {
			return state, err
		}
	}
	if err := updateOrderItemAmountsTx(ctx, tx, plan.Rows); err != nil {
		return state, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE his_orders
		SET price_identity = $2, items_total = $3, identity_discount_total = $4,
		    negotiation_amount = $5, discount_total = $6, payable_total = $7, updated_at = NOW()
		WHERE id = $1`,
		orderID, identity, plan.ItemsTotal, plan.IdentityDiscount,
		plan.NegotiationTotal, plan.DiscountTotal, plan.PayableTotal); err != nil {
		return state, err
	}
	state.PayableTotal = plan.PayableTotal
	state.Repriced = orderIdentity != identity || discountMoney(payable) != plan.PayableTotal
	if state.Repriced {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_order_repricing_logs
				(tenant_id, order_id, customer_id, previous_identity, new_identity,
				 previous_payable, payable_total, operator_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			tenantID, orderID, customerID, orderIdentity, identity,
			payable, plan.PayableTotal, operatorID); err != nil {
			return state, err
		}
	}
	return state, nil
}

// PreviewOrderReprice 计算 pending 单按当前身份重算后的金额，不写库。
// 用于收费弹窗在提交前提示「原 ¥X → ¥Y」。
func (s *Store) PreviewOrderReprice(ctx context.Context, tenantID, orderID int64) (OrderRepricePreview, error) {
	var preview OrderRepricePreview
	var status, orderIdentity string
	var customerID int64
	var payable, paid, itemsTotal, identityDiscount, negotiationAmount, discountTotal float64
	err := s.pool.QueryRow(ctx, `
		SELECT status, customer_id, payable_total, paid_total,
		       COALESCE(NULLIF(price_identity, ''), 'standard'),
		       items_total, identity_discount_total, negotiation_amount, discount_total
		FROM his_orders WHERE tenant_id = $1 AND id = $2`,
		tenantID, orderID).Scan(&status, &customerID, &payable, &paid, &orderIdentity,
		&itemsTotal, &identityDiscount, &negotiationAmount, &discountTotal)
	if err != nil {
		return preview, err
	}
	preview.PreviousPayable = payable
	preview.PayableTotal = payable
	preview.PreviousIdentity = orderIdentity
	preview.PriceIdentity = orderIdentity
	preview.PaidTotal = paid
	preview.ItemsTotal = itemsTotal
	preview.IdentityDiscountTotal = identityDiscount
	preview.NegotiationAmount = negotiationAmount
	preview.DiscountTotal = discountTotal

	identity, rates, err := s.discountContextTx(ctx, s.pool, tenantID, customerID)
	if err != nil {
		return preview, err
	}
	preview.PriceIdentity = identity
	if err := checkOrderIdentityChangeAllowed(status, orderIdentity, identity); err != nil {
		return preview, err
	}
	if !shouldRepriceOrder(status) {
		return preview, nil
	}
	rows, err := s.loadOrderItemRowsTx(ctx, s.pool, orderID)
	if err != nil {
		return preview, err
	}
	negotiation, err := loadOrderNegotiationTx(ctx, s.pool, orderID)
	if err != nil {
		return preview, err
	}
	plan, err := planOrderReprice(rows, negotiation, rates)
	if err != nil {
		return preview, err
	}
	preview.PayableTotal = plan.PayableTotal
	preview.ItemsTotal = plan.ItemsTotal
	preview.IdentityDiscountTotal = plan.IdentityDiscount
	preview.NegotiationAmount = plan.NegotiationTotal
	preview.DiscountTotal = plan.DiscountTotal
	preview.Repriced = orderIdentity != identity || discountMoney(payable) != plan.PayableTotal
	return preview, nil
}

// ListOrderRepricingLogs 读取订单重算留痕（最近 N 条），用于对账/排查。
func (s *Store) ListOrderRepricingLogs(ctx context.Context, tenantID, orderID int64, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, previous_identity, new_identity, previous_payable, payable_total,
		       COALESCE(operator_id, 0), created_at
		FROM his_order_repricing_logs
		WHERE tenant_id = $1 AND order_id = $2
		ORDER BY id DESC LIMIT $3`, tenantID, orderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]map[string]any, 0)
	for rows.Next() {
		var id, operatorID int64
		var previousIdentity, newIdentity string
		var previousPayable, payableTotal float64
		var createdAt any
		if err := rows.Scan(&id, &previousIdentity, &newIdentity, &previousPayable,
			&payableTotal, &operatorID, &createdAt); err != nil {
			return nil, err
		}
		logs = append(logs, map[string]any{
			"id": id, "previous_identity": previousIdentity, "new_identity": newIdentity,
			"previous_payable": previousPayable, "payable_total": payableTotal,
			"operator_id": operatorID, "created_at": createdAt,
		})
	}
	return logs, rows.Err()
}
