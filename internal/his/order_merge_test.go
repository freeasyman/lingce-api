package his

import (
	"errors"
	"testing"
)

// 收费单合并的纯逻辑测试：去重键、有/无未结清单两条路径、合并后金额重算、折扣校验。

func rowRef(v int64) *int64 { return &v }

func testOrderRow(itemType string, refID, prescriptionID *int64, unitPrice, quantity, discount, lineTotal float64) orderItemRow {
	return orderItemRow{
		ItemType:       itemType,
		RefID:          refID,
		PrescriptionID: prescriptionID,
		Name:           itemType,
		UnitPrice:      unitPrice,
		Quantity:       quantity,
		DiscountAmount: discount,
		LineTotal:      lineTotal,
	}
}

func TestMergeOrderItemRowsDedupesByTypeRefPrescription(t *testing.T) {
	existing := []orderItemRow{
		testOrderRow(orderItemRegistrationFee, nil, nil, 12, 1, 0, 12),
		testOrderRow(orderItemChargeItem, rowRef(9), nil, 25, 1, 0, 25),
		testOrderRow(orderItemDrug, rowRef(2), rowRef(77), 18.4, 2, 0, 36.8),
	}
	incoming := []orderItemRow{
		// 同挂号费、同收费项目、同处方药行：全部跳过，不重复加。
		testOrderRow(orderItemRegistrationFee, nil, nil, 20, 1, 0, 20),
		testOrderRow(orderItemChargeItem, rowRef(9), nil, 25, 2, 0, 50),
		testOrderRow(orderItemDrug, rowRef(2), rowRef(77), 18.4, 2, 0, 36.8),
		// 同药品但不同处方：处方维度不同，属于新行。
		testOrderRow(orderItemDrug, rowRef(2), rowRef(78), 18.4, 1, 0, 18.4),
		// 新检查项目：新增。
		testOrderRow(orderItemChargeItem, rowRef(10), nil, 80, 1, 0, 80),
		// 本次入参内部重复：只保留一条。
		testOrderRow(orderItemChargeItem, rowRef(10), nil, 80, 1, 0, 80),
	}

	insertRows := mergeOrderItemRows(existing, incoming)
	if len(insertRows) != 2 {
		t.Fatalf("insert rows = %d (%+v), want 2 (drug rx78 + exam 10)", len(insertRows), insertRows)
	}
	if insertRows[0].ItemType != orderItemDrug || insertRows[0].PrescriptionID == nil || *insertRows[0].PrescriptionID != 78 {
		t.Fatalf("first insert = %+v, want drug rx78", insertRows[0])
	}
	if insertRows[1].ItemType != orderItemChargeItem || insertRows[1].RefID == nil || *insertRows[1].RefID != 10 {
		t.Fatalf("second insert = %+v, want charge item 10", insertRows[1])
	}
}

func TestPlanOrderMergeWithoutOpenOrderCreatesAllRows(t *testing.T) {
	// 无未结清单：走动新建单路径，本次行全部落库。
	incoming := []orderItemRow{
		testOrderRow(orderItemRegistrationFee, nil, nil, 12, 1, 0, 12),
		testOrderRow(orderItemDrug, rowRef(2), rowRef(77), 18.4, 2, 0, 36.8),
	}
	plan, err := planOrderMerge(nil, incoming, 4.5)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.InsertRows) != 2 {
		t.Fatalf("insert rows = %d, want 2", len(plan.InsertRows))
	}
	if plan.ItemsTotal != 48.8 {
		t.Fatalf("items total = %v, want 48.8", plan.ItemsTotal)
	}
	if plan.PayableTotal != 44.3 {
		t.Fatalf("payable = %v, want 44.3", plan.PayableTotal)
	}
}

func TestPlanOrderMergeWithOpenOrderDedupesAndRecalculates(t *testing.T) {
	// 已有未结清单（检验+检查）：合并挂号费/处方，全部保留，金额重算。
	existing := []orderItemRow{
		testOrderRow(orderItemChargeItem, rowRef(9), nil, 25, 1, 0, 25),
		testOrderRow(orderItemChargeItem, rowRef(10), nil, 80, 1, 0, 80),
	}
	incoming := []orderItemRow{
		testOrderRow(orderItemChargeItem, rowRef(9), nil, 25, 1, 0, 25), // 已在单内，跳过
		testOrderRow(orderItemRegistrationFee, nil, nil, 12, 1, 0, 12),
		testOrderRow(orderItemDrug, rowRef(2), rowRef(77), 18.4, 1, 0, 18.4),
	}
	plan, err := planOrderMerge(existing, incoming, 0)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.InsertRows) != 2 {
		t.Fatalf("insert rows = %d (%+v), want 2 (fee + drug)", len(plan.InsertRows), plan.InsertRows)
	}
	if plan.ItemsTotal != 135.4 {
		t.Fatalf("items total = %v, want 135.4 (25+80+12+18.4)", plan.ItemsTotal)
	}
	if plan.PayableTotal != 135.4 {
		t.Fatalf("payable = %v, want 135.4", plan.PayableTotal)
	}
}

func TestPlanOrderMergeUsesStoredLineTotal(t *testing.T) {
	// 金额重算以已落库的 line_total（行优惠后）为准，不用单价×数量重算历史行。
	existing := []orderItemRow{
		testOrderRow(orderItemChargeItem, rowRef(9), nil, 25, 2, 5, 45),
	}
	plan, err := planOrderMerge(existing, nil, 5)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.ItemsTotal != 45 {
		t.Fatalf("items total = %v, want 45", plan.ItemsTotal)
	}
	if plan.PayableTotal != 40 {
		t.Fatalf("payable = %v, want 40 (45 - 5 order discount)", plan.PayableTotal)
	}
}

func TestPlanOrderMergeRejectsNegativeDiscount(t *testing.T) {
	_, err := planOrderMerge(nil, []orderItemRow{
		testOrderRow(orderItemRegistrationFee, nil, nil, 12, 1, 0, 12),
	}, -1)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative discount err = %v, want ErrInvalidRequest", err)
	}
}

func TestOrderItemRowKeyRegistrationFee(t *testing.T) {
	// 挂号费没有 ref_id/prescription_id：同单只保留一条，避免合并后重复收挂号费。
	key := orderItemRowKey(testOrderRow(orderItemRegistrationFee, nil, nil, 12, 1, 0, 12))
	if key != (orderItemKey{ItemType: orderItemRegistrationFee}) {
		t.Fatalf("registration fee key = %+v", key)
	}
}
