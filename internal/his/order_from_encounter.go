package his

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// orderDraftLine 是按就诊组装收费单时的中间行（价格由服务端解析，不收前端传价）。
type orderDraftLine struct {
	ItemType       string
	RefID          *int64
	PrescriptionID *int64
	Name           string
	UnitPrice      float64
	Quantity       float64
	DiscountAmount float64
}

// buildEncounterOrderLines 把挂号费与处方明细组装为收费行。
//
//   - includeFee 为 true 且 feeAmount > 0 时，首行生成挂号费（item_type=charge_item，
//     若字典存在“挂号费”则带 ref_id，否则允许 ref_id 为空）。
//   - selectedIDs 为空表示取该就诊全部 draft/issued 处方；非空时必须都是有效处方。
//   - overrides 非空时按收银台改量后的 drug 行生成（数量来自收银台，单价仍取处方价）。
func buildEncounterOrderLines(
	feeAmount float64,
	feeItemID *int64,
	includeFee bool,
	prescriptions []Prescription,
	selectedIDs []int64,
	overrides []OrderLineInput,
) ([]orderDraftLine, error) {
	if feeAmount < 0 {
		return nil, ErrInvalidRequest
	}
	selected := make(map[int64]bool, len(selectedIDs))
	for _, id := range selectedIDs {
		if id <= 0 {
			return nil, ErrInvalidRequest
		}
		selected[id] = true
	}

	available := make(map[int64]Prescription)
	chosen := make([]Prescription, 0, len(prescriptions))
	for _, rx := range prescriptions {
		if rx.Status != "draft" && rx.Status != "issued" {
			if selected[rx.ID] {
				return nil, ErrInvalidRequest
			}
			continue
		}
		available[rx.ID] = rx
		if len(selected) == 0 || selected[rx.ID] {
			chosen = append(chosen, rx)
		}
	}
	for id := range selected {
		if _, ok := available[id]; !ok {
			return nil, ErrInvalidRequest
		}
	}

	lines := make([]orderDraftLine, 0, len(overrides)+1)
	if includeFee && feeAmount > 0 {
		lines = append(lines, orderDraftLine{
			ItemType: "charge_item", RefID: feeItemID, Name: "挂号费",
			UnitPrice: feeAmount, Quantity: 1,
		})
	}

	if len(overrides) > 0 {
		for _, input := range overrides {
			if input.ItemType != "drug" || input.RefID <= 0 || input.Quantity <= 0 || input.PrescriptionID == nil {
				return nil, ErrInvalidRequest
			}
			rx, ok := available[*input.PrescriptionID]
			if !ok || (len(selected) > 0 && !selected[rx.ID]) {
				return nil, ErrInvalidRequest
			}
			var matched *PrescriptionItem
			for index := range rx.Items {
				item := &rx.Items[index]
				if item.DrugID != nil && *item.DrugID == input.RefID {
					matched = item
					break
				}
			}
			if matched == nil {
				return nil, ErrInvalidRequest
			}
			line, err := newOrderDraftLine(input.ItemType, input.RefID, rx.ID, matched.Name,
				matched.Price, input.Quantity, input.DiscountAmount)
			if err != nil {
				return nil, err
			}
			lines = append(lines, line)
		}
	} else {
		for _, rx := range chosen {
			for _, item := range rx.Items {
				if item.DrugID == nil {
					continue
				}
				line, err := newOrderDraftLine("drug", *item.DrugID, rx.ID, item.Name,
					item.Price, item.Quantity, 0)
				if err != nil {
					return nil, err
				}
				lines = append(lines, line)
			}
		}
	}

	if len(lines) == 0 {
		return nil, ErrInvalidRequest
	}
	return lines, nil
}

func newOrderDraftLine(itemType string, refID, prescriptionID int64, name string, unitPrice, quantity, discount float64) (orderDraftLine, error) {
	if quantity <= 0 || discount < 0 || unitPrice < 0 {
		return orderDraftLine{}, ErrInvalidRequest
	}
	if unitPrice*quantity-discount < 0 {
		return orderDraftLine{}, ErrInvalidRequest
	}
	ref := refID
	prescription := prescriptionID
	return orderDraftLine{
		ItemType: itemType, RefID: &ref, PrescriptionID: &prescription, Name: name,
		UnitPrice: unitPrice, Quantity: quantity, DiscountAmount: discount,
	}, nil
}

// CreateOrderFromEncounter 按就诊一键生成收费单（pending）。
func (s *Service) CreateOrderFromEncounter(ctx context.Context, tenantID, createdBy int64, req CreateOrderFromEncounterRequest) (Order, error) {
	if req.EncounterID <= 0 || req.DiscountTotal < 0 {
		return Order{}, ErrInvalidRequest
	}
	if !req.IncludeRegistrationFee && len(req.PrescriptionIDs) == 0 && len(req.Items) == 0 {
		return Order{}, ErrInvalidRequest
	}
	for _, item := range req.Items {
		if item.ItemType != "drug" || item.RefID <= 0 || item.Quantity <= 0 || item.PrescriptionID == nil {
			return Order{}, ErrInvalidRequest
		}
	}
	return s.store.CreateOrderFromEncounter(ctx, tenantID, createdBy, req)
}

// CreateOrderFromEncounter 读取就诊挂号费与处方，生成完整收费单。
func (s *Store) CreateOrderFromEncounter(ctx context.Context, tenantID, createdBy int64, req CreateOrderFromEncounterRequest) (Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Order{}, err
	}
	defer tx.Rollback(ctx)

	var customerID int64
	var registrationFee float64
	err = tx.QueryRow(ctx, `
		SELECT r.customer_id, r.fee
		FROM encounters e
		JOIN his_registrations r ON r.tenant_id = e.tenant_id AND r.id = e.source_id
		WHERE e.tenant_id = $1 AND e.id = $2 AND e.source_type = 'his'`,
		tenantID, req.EncounterID).Scan(&customerID, &registrationFee)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Order{}, ErrNotFound
		}
		return Order{}, err
	}

	var feeItemID *int64
	var matchedFeeItem int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM his_charge_items
		WHERE tenant_id = $1 AND name = '挂号费' AND status = 'active'
		ORDER BY id LIMIT 1`, tenantID).Scan(&matchedFeeItem)
	if err == nil {
		feeItemID = &matchedFeeItem
	} else if err != pgx.ErrNoRows {
		return Order{}, err
	}

	prescriptions, err := s.prescriptionsForEncounterTx(ctx, tx, tenantID, req.EncounterID)
	if err != nil {
		return Order{}, err
	}
	lines, err := buildEncounterOrderLines(registrationFee, feeItemID, req.IncludeRegistrationFee,
		prescriptions, req.PrescriptionIDs, req.Items)
	if err != nil {
		return Order{}, err
	}

	seq, err := s.NextNumber(ctx, tenantID, "order")
	if err != nil {
		return Order{}, err
	}
	orderNo := fmt.Sprintf("O%s%04d", time.Now().Format("20060102"), seq)
	var orderID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO his_orders (tenant_id, customer_id, encounter_id, order_no, status, created_by)
		VALUES ($1, $2, $3, $4, 'pending', $5) RETURNING id`,
		tenantID, customerID, req.EncounterID, orderNo, createdBy).Scan(&orderID); err != nil {
		return Order{}, err
	}
	for _, line := range lines {
		lineTotal := line.UnitPrice*line.Quantity - line.DiscountAmount
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_order_items (order_id, item_type, ref_id, prescription_id, name, unit_price,
				quantity, line_total, discount_amount, payable)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $8)`,
			orderID, line.ItemType, line.RefID, line.PrescriptionID, line.Name, line.UnitPrice,
			line.Quantity, lineTotal, line.DiscountAmount); err != nil {
			return Order{}, err
		}
	}
	if err := recomputeOrderTotals(ctx, tx, orderID, req.DiscountTotal); err != nil {
		return Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Order{}, err
	}
	return s.loadOrder(ctx, s.pool, tenantID, orderID)
}

// prescriptionsForEncounterTx 事务内读取就诊处方及明细。
// 注意：同一事务连接上必须先读完并关闭结果集，再逐条补明细，否则 pgx 会报 conn busy。
func (s *Store) prescriptionsForEncounterTx(ctx context.Context, q querier, tenantID, encounterID int64) ([]Prescription, error) {
	rows, err := q.Query(ctx, `
		SELECT `+prescriptionColumns+` FROM his_prescriptions
		WHERE tenant_id = $1 AND encounter_id = $2 ORDER BY id`, tenantID, encounterID)
	if err != nil {
		return nil, err
	}

	items := make([]Prescription, 0)
	for rows.Next() {
		var p Prescription
		if err := rows.Scan(&p.ID, &p.EncounterID, &p.CustomerID, &p.DoctorID, &p.PrescriptionNo,
			&p.Type, &p.Diagnosis, &p.Status, &p.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range items {
		full, err := s.prescriptionWithItems(ctx, q, items[index])
		if err != nil {
			return nil, err
		}
		items[index] = full
	}
	return items, nil
}
