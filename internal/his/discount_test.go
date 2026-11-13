package his

import (
	"context"
	"errors"
	"testing"
)

// 折扣大类映射：药品/材料/挂号/诊查/检查/检验/治疗/护理/其他。
func TestDiscountCategoryMapping(t *testing.T) {
	cases := []struct {
		itemType string
		category string
		want     string
	}{
		{orderItemRegistrationFee, "", discountCategoryRegister},
		{orderItemDrug, "", discountCategoryDrug},
		{orderItemChargeItem, "材料", discountCategoryMaterial},
		{orderItemChargeItem, "耗材", discountCategoryMaterial},
		{orderItemChargeItem, "诊查", discountCategoryConsult},
		{orderItemChargeItem, "诊费", discountCategoryConsult},
		{orderItemChargeItem, "检查", discountCategoryExam},
		{orderItemChargeItem, "检验", discountCategoryLab},
		{orderItemChargeItem, "治疗理疗", discountCategoryTreatment},
		{orderItemChargeItem, "护理", discountCategoryNursing},
		{orderItemChargeItem, "病理", discountCategoryOther},
		{orderItemChargeItem, "输氧", discountCategoryOther},
		{orderItemChargeItem, "其他费用", discountCategoryOther},
	}
	for _, testCase := range cases {
		if got := discountCategoryForLine(testCase.itemType, testCase.category); got != testCase.want {
			t.Fatalf("category(%s,%s) = %s, want %s", testCase.itemType, testCase.category, got, testCase.want)
		}
	}
}

// 药品锁定 1.0；材料允许 <1；其他大类允许 <1；越界拒绝。
func TestNormalizeDiscountRates(t *testing.T) {
	if _, err := normalizeDiscountRates(map[string]float64{discountCategoryDrug: 0.9}); !errors.Is(err, ErrDrugDiscountLocked) {
		t.Fatalf("drug <1 err = %v, want ErrDrugDiscountLocked", err)
	}
	if _, err := normalizeDiscountRates(map[string]float64{discountCategoryDrug: 1, discountCategoryMaterial: 0.8}); err != nil {
		t.Fatalf("material <1 should be allowed: %v", err)
	}
	if _, err := normalizeDiscountRates(map[string]float64{discountCategoryOther: 0.5}); err != nil {
		t.Fatalf("other <1 should be allowed: %v", err)
	}
	for _, rate := range []float64{0, -0.1, 1.01} {
		if _, err := normalizeDiscountRates(map[string]float64{discountCategoryConsult: rate}); !errors.Is(err, ErrInvalidDiscountRate) {
			t.Fatalf("rate %v err = %v, want ErrInvalidDiscountRate", rate, err)
		}
	}
	if _, err := normalizeDiscountRates(map[string]float64{"未知": 0.5}); !errors.Is(err, ErrInvalidDiscountRate) {
		t.Fatalf("unknown category err = %v, want ErrInvalidDiscountRate", err)
	}
}

func testDiscountRow(category string, original, rate float64, negotiable bool) orderItemRow {
	row := orderItemRow{
		ItemType:      orderItemChargeItem,
		Category:      category,
		UnitPrice:     original,
		Quantity:      1,
		OriginalTotal: original,
		IdentityRate:  rate,
	}
	row.DiscountAmount = discountMoney(original * (1 - rate))
	row.LineTotal = discountMoney(original - row.DiscountAmount)
	return row
}

// 议价只摊可折行：药品/材料不参与。
func TestApplyNegotiationOnlyDiscountable(t *testing.T) {
	rows := []orderItemRow{
		testDiscountRow(discountCategoryConsult, 100, 0.7, true),  // 70
		testDiscountRow(discountCategoryDrug, 50, 1, false),       // 50
		testDiscountRow(discountCategoryExam, 200, 0.85, true),    // 170
		testDiscountRow(discountCategoryMaterial, 30, 0.8, false), // 24
	}
	total, err := applyNegotiation(rows, negotiationModePercent, 10)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if total != 24 {
		t.Fatalf("negotiation total = %v, want 24 (10%% of 240)", total)
	}
	if rows[1].NegotiationAmount != 0 || rows[3].NegotiationAmount != 0 {
		t.Fatalf("drug/material should not be negotiated: %+v %+v", rows[1], rows[3])
	}
	if rows[0].NegotiationAmount != 7 || rows[0].LineTotal != 63 {
		t.Fatalf("consult row = %+v, want neg 7 / payable 63", rows[0])
	}
	if rows[2].NegotiationAmount != 17 || rows[2].LineTotal != 153 {
		t.Fatalf("exam row = %+v, want neg 17 / payable 153", rows[2])
	}
	if got := orderRowsPayable(rows); got != 290 {
		t.Fatalf("payable = %v, want 290 (63+50+153+24)", got)
	}
}

// 金额议价分摊到可折行；余数给最大行（分单位不丢）。
func TestAllocateNegotiationRemainderToLargest(t *testing.T) {
	allocations := allocateNegotiation(1.00, []float64{1, 1, 1})
	sum := 0.0
	for _, value := range allocations {
		sum += value
	}
	if discountMoney(sum) != 1 {
		t.Fatalf("allocations = %v, want sum 1", allocations)
	}
	if allocations[0] != 0.34 {
		t.Fatalf("largest/first row = %v, want 0.34 (remainder to largest)", allocations[0])
	}
}

// 身份折率：员工诊查 0.7 / 检查 0.85 / 药品 1.0 → 100/200/50 应收 290。
func TestOrderIdentityDiscountsAndNegotiation(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	consult, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "CONS01", Name: "主任诊查费", Category: "诊查", Unit: "次", Price: 100})
	if err != nil {
		t.Fatal(err)
	}
	exam, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "EXAM01", Name: "腹部彩超", Category: "检查", Unit: "次", Price: 200})
	if err != nil {
		t.Fatal(err)
	}
	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{Code: "DRUG01", Name: "阿莫西林", Spec: "0.25g*24", Unit: "盒", DrugType: "western", RetailPrice: 50})
	if err != nil {
		t.Fatal(err)
	}

	encounterID, patient := walkinEncounter(t, svc, 1)
	if err := svc.SetPatientPriceIdentity(ctx, 1, patient.CustomerID, 10, priceIdentityStaff); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityStaff, map[string]float64{
		discountCategoryConsult: 0.7, discountCategoryExam: 0.85, discountCategoryDrug: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityStaff, map[string]float64{discountCategoryDrug: 0.9}); !errors.Is(err, ErrDrugDiscountLocked) {
		t.Fatalf("drug <1 save err = %v, want ErrDrugDiscountLocked", err)
	}

	items := []OrderLineInput{
		{ItemType: orderItemChargeItem, RefID: consult.ID, Quantity: 1},
		{ItemType: orderItemDrug, RefID: drug.ID, Quantity: 1},
		{ItemType: orderItemChargeItem, RefID: exam.ID, Quantity: 1},
	}
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{EncounterID: encounterID, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	if order.ItemsTotal != 350 || order.IdentityDiscountTotal != 60 || order.PayableTotal != 290 {
		t.Fatalf("order totals = items %v identity %v payable %v, want 350/60/290",
			order.ItemsTotal, order.IdentityDiscountTotal, order.PayableTotal)
	}
	if order.PriceIdentity != priceIdentityStaff {
		t.Fatalf("order identity = %s, want staff", order.PriceIdentity)
	}
	for _, item := range order.Items {
		switch item.Category {
		case discountCategoryDrug:
			if item.IdentityRate != 1 || item.DiscountAmount != 0 || item.Payable != 50 {
				t.Fatalf("drug line = %+v, want rate1/discount0/payable50", item)
			}
		case discountCategoryConsult:
			if item.IdentityRate != 0.7 || item.DiscountAmount != 30 || item.Payable != 70 {
				t.Fatalf("consult line = %+v, want 0.7/30/70", item)
			}
		case discountCategoryExam:
			if item.IdentityRate != 0.85 || item.DiscountAmount != 30 || item.Payable != 170 {
				t.Fatalf("exam line = %+v, want 0.85/30/170", item)
			}
		}
	}

	// 议价 9 折（让 10%）：只折诊查+检查 (70+170=240 → 216)，药品不变 → 266。
	negotiated, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Negotiation: &NegotiationInput{Mode: negotiationModePercent, Value: 10, Reason: "员工内部优惠"},
		Items:       items,
	})
	if err != nil {
		t.Fatal(err)
	}
	if negotiated.NegotiationAmount != 24 || negotiated.PayableTotal != 266 || negotiated.DiscountTotal != 84 {
		t.Fatalf("negotiated = neg %v payable %v discount %v, want 24/266/84",
			negotiated.NegotiationAmount, negotiated.PayableTotal, negotiated.DiscountTotal)
	}
	for _, item := range negotiated.Items {
		if item.Category == discountCategoryDrug && (item.NegotiationAmount != 0 || item.Payable != 50) {
			t.Fatalf("drug line should not be negotiated: %+v", item)
		}
	}

	// 议价必须填原因。
	if _, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Negotiation: &NegotiationInput{Mode: negotiationModeAmount, Value: 10},
		Items:       items,
	}); !errors.Is(err, ErrNegotiationReasonRequired) {
		t.Fatalf("missing reason err = %v, want ErrNegotiationReasonRequired", err)
	}
}

// 退费按折后金额退：员工检查 200×0.85=170。
func TestRefundUsesDiscountedPayable(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	exam, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{Code: "EXAM02", Name: "胸部 CT", Category: "检查", Unit: "次", Price: 200})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, patient := walkinEncounter(t, svc, 1)
	if err := svc.SetPatientPriceIdentity(ctx, 1, patient.CustomerID, 10, priceIdentityStaff); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityStaff, map[string]float64{discountCategoryExam: 0.85}); err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: exam.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PayableTotal != 170 {
		t.Fatalf("payable = %v, want 170", order.PayableTotal)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 170}); err != nil {
		t.Fatal(err)
	}
	refunded, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "检查取消", Items: []RefundItemInput{{OrderItemID: order.Items[0].ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if refunded.RefundedTotal != 170 {
		t.Fatalf("refunded total = %v, want 170", refunded.RefundedTotal)
	}
}

// 普通身份全 1.0；材料可配 <1；其他可配。
func TestStandardIdentityDefaultsAndMaterialDiscount(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	standard, err := svc.DiscountMatrix(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for identity, rates := range standard.Rules {
		for category, rate := range rates {
			if rate != 1 {
				t.Fatalf("default %s/%s = %v, want 1", identity, category, rate)
			}
		}
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityMember, map[string]float64{
		discountCategoryMaterial: 0.8, discountCategoryOther: 0.9,
	}); err != nil {
		t.Fatal(err)
	}
	matrix, err := svc.DiscountMatrix(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Rules[priceIdentityMember][discountCategoryMaterial] != 0.8 ||
		matrix.Rules[priceIdentityMember][discountCategoryOther] != 0.9 ||
		matrix.Rules[priceIdentityMember][discountCategoryDrug] != 1 {
		t.Fatalf("member matrix = %+v", matrix.Rules[priceIdentityMember])
	}
}

// 议价权限：管理员/店长不限；普通收费员受租户上限约束。
func TestNegotiationPermissionLimit(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO his_staff_roles (tenant_id, employee_id, role) VALUES (1, 10, 'nurse')`); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateCashierMaxNegotiationRate(ctx, 1, 0.05); err != nil {
		t.Fatal(err)
	}
	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "NEG01", Name: "微波治疗", Category: "治疗", Unit: "次", Price: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	items := []OrderLineInput{{ItemType: orderItemChargeItem, RefID: item.ID, Quantity: 1}}

	// 收费员让利 10% 超过 5% 上限 → 拒绝。
	if _, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Negotiation: &NegotiationInput{Mode: negotiationModePercent, Value: 10, Reason: "超限"},
		Items:       items,
	}); !errors.Is(err, ErrNegotiationForbidden) {
		t.Fatalf("10%% as nurse err = %v, want ErrNegotiationForbidden", err)
	}
	// 5% 在上限内 → 通过，应收 95。
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Negotiation: &NegotiationInput{Mode: negotiationModePercent, Value: 5, Reason: "权限内"},
		Items:       items,
	})
	if err != nil {
		t.Fatalf("5%% as nurse: %v", err)
	}
	if order.PayableTotal != 95 {
		t.Fatalf("payable = %v, want 95", order.PayableTotal)
	}

	// 管理员不受上限约束。
	if _, err := pool.Exec(ctx, `DELETE FROM his_staff_roles WHERE tenant_id = 1 AND employee_id = 10`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO his_staff_roles (tenant_id, employee_id, role) VALUES (1, 10, 'admin')`); err != nil {
		t.Fatal(err)
	}
	adminOrder, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Negotiation: &NegotiationInput{Mode: negotiationModePercent, Value: 50, Reason: "管理员特批"},
		Items:       items,
	})
	if err != nil {
		t.Fatalf("admin 50%%: %v", err)
	}
	if adminOrder.PayableTotal != 50 {
		t.Fatalf("admin payable = %v, want 50", adminOrder.PayableTotal)
	}
}

// 协议默认取会员价；单独配置后覆盖。
func TestAgreementDefaultsToMemberPrice(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityMember, map[string]float64{
		discountCategoryExam: 0.8,
	}); err != nil {
		t.Fatal(err)
	}
	matrix, err := svc.DiscountMatrix(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Rules[priceIdentityAgreement][discountCategoryExam] != 0.8 {
		t.Fatalf("agreement exam = %v, want member 0.8", matrix.Rules[priceIdentityAgreement][discountCategoryExam])
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityAgreement, map[string]float64{
		discountCategoryExam: 0.9,
	}); err != nil {
		t.Fatal(err)
	}
	matrix, err = svc.DiscountMatrix(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if matrix.Rules[priceIdentityAgreement][discountCategoryExam] != 0.9 {
		t.Fatalf("agreement exam = %v, want 0.9 after override", matrix.Rules[priceIdentityAgreement][discountCategoryExam])
	}
}
