package his

import (
	"errors"
	"testing"
)

func repriceTestRow(category string, price float64) orderItemRow {
	return orderItemRow{
		ItemType:  orderItemChargeItem,
		Name:      category,
		Category:  category,
		UnitPrice: price,
		Quantity:  1,
	}
}

func repriceDrugRow(price float64) orderItemRow {
	return orderItemRow{
		ItemType:  orderItemDrug,
		Name:      discountCategoryDrug,
		Category:  discountCategoryDrug,
		UnitPrice: price,
		Quantity:  1,
	}
}

func staffTestRates() map[string]float64 {
	rates := defaultDiscountRates()
	rates[discountCategoryExam] = 0.85
	rates[discountCategoryLab] = 1
	rates[discountCategoryDrug] = 1
	return rates
}

// 普通 → 员工：检验 25→25，检查 80→68，应收 105→93。
func TestPlanOrderRepriceStandardToStaff(t *testing.T) {
	rows := []orderItemRow{
		repriceTestRow(discountCategoryLab, 25),
		repriceTestRow(discountCategoryExam, 80),
	}
	standard, err := planOrderReprice(rows, orderNegotiation{}, defaultDiscountRates())
	if err != nil {
		t.Fatalf("standard plan: %v", err)
	}
	if standard.PayableTotal != 105 || standard.IdentityDiscount != 0 {
		t.Fatalf("standard payable/discount = %v/%v, want 105/0", standard.PayableTotal, standard.IdentityDiscount)
	}
	staff, err := planOrderReprice(rows, orderNegotiation{}, staffTestRates())
	if err != nil {
		t.Fatalf("staff plan: %v", err)
	}
	if staff.PayableTotal != 93 || staff.IdentityDiscount != 12 {
		t.Fatalf("staff payable/discount = %v/%v, want 93/12", staff.PayableTotal, staff.IdentityDiscount)
	}
	for _, row := range staff.Rows {
		switch row.Category {
		case discountCategoryLab:
			if row.IdentityRate != 1 || row.DiscountAmount != 0 || row.LineTotal != 25 {
				t.Fatalf("lab row = %+v, want rate1/discount0/payable25", row)
			}
		case discountCategoryExam:
			if row.IdentityRate != 0.85 || row.DiscountAmount != 12 || row.LineTotal != 68 {
				t.Fatalf("exam row = %+v, want rate.85/discount12/payable68", row)
			}
		}
	}
	// 原输入不被就地修改。
	if rows[1].IdentityRate != 0 || rows[1].LineTotal != 0 {
		t.Fatalf("input rows mutated: %+v", rows[1])
	}
}

// 议价保留 amount 值，按新的可折金额重新分摊（105 基数下 10 元 → 93 基数下 10 元）。
func TestPlanOrderRepriceReallocatesAmountNegotiation(t *testing.T) {
	rows := []orderItemRow{
		repriceTestRow(discountCategoryLab, 25),
		repriceTestRow(discountCategoryExam, 80),
	}
	negotiation := orderNegotiation{Mode: negotiationModeAmount, Value: 10, Reason: "员工优惠"}
	plan, err := planOrderReprice(rows, negotiation, staffTestRates())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.NegotiationTotal != 10 {
		t.Fatalf("negotiation = %v, want 10", plan.NegotiationTotal)
	}
	if plan.PayableTotal != 83 || plan.DiscountTotal != 22 {
		t.Fatalf("payable/discount = %v/%v, want 83/22", plan.PayableTotal, plan.DiscountTotal)
	}
	sum := 0.0
	for _, row := range plan.Rows {
		sum += row.NegotiationAmount
	}
	if discountMoney(sum) != 10 {
		t.Fatalf("line negotiation sum = %v, want 10", sum)
	}
	for _, row := range plan.Rows {
		if row.LineTotal != discountMoney(row.OriginalTotal-row.DiscountAmount-row.NegotiationAmount) {
			t.Fatalf("row %s line total mismatch: %+v", row.Category, row)
		}
	}
}

// 百分比议价按新的可折金额重算：93 的 10% = 9.30。
func TestPlanOrderRepriceReallocatesPercentNegotiation(t *testing.T) {
	rows := []orderItemRow{
		repriceTestRow(discountCategoryLab, 25),
		repriceTestRow(discountCategoryExam, 80),
	}
	plan, err := planOrderReprice(rows, orderNegotiation{Mode: negotiationModePercent, Value: 10}, staffTestRates())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.NegotiationTotal != 9.3 || plan.PayableTotal != 83.7 {
		t.Fatalf("negotiation/payable = %v/%v, want 9.3/83.7", plan.NegotiationTotal, plan.PayableTotal)
	}
}

// 药品锁 1.0；材料按当前矩阵；金额议价超过可折金额时收口。
func TestPlanOrderRepriceDrugLockedAndAmountCapped(t *testing.T) {
	rates := staffTestRates()
	rates[discountCategoryDrug] = 0.5 // 脏数据也必须被锁回 1.0
	rates[discountCategoryMaterial] = 0.8
	rows := []orderItemRow{
		repriceDrugRow(100),
		repriceTestRow(discountCategoryMaterial, 50),
		repriceTestRow(discountCategoryExam, 80),
	}
	plan, err := planOrderReprice(rows, orderNegotiation{}, rates)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	for _, row := range plan.Rows {
		if row.Category == discountCategoryDrug && (row.IdentityRate != 1 || row.LineTotal != 100) {
			t.Fatalf("drug row = %+v, want rate1/payable100", row)
		}
		if row.Category == discountCategoryMaterial && (row.IdentityRate != 0.8 || row.LineTotal != 40) {
			t.Fatalf("material row = %+v, want rate.8/payable40", row)
		}
	}
	// 可折金额 = 检查 68（药品/材料不参与议价）；金额议价 200 收口到 68。
	capped, err := planOrderReprice(rows, orderNegotiation{Mode: negotiationModeAmount, Value: 200}, rates)
	if err != nil {
		t.Fatalf("capped plan: %v", err)
	}
	if capped.NegotiationTotal != 68 || capped.PayableTotal != 140 {
		t.Fatalf("capped neg/payable = %v/%v, want 68/140", capped.NegotiationTotal, capped.PayableTotal)
	}
}

// paid 不重算、partial 身份变更拒绝、partial 身份未变允许补收。
func TestRepriceStatusGuard(t *testing.T) {
	if shouldRepriceOrder("paid") || shouldRepriceOrder("partial") || shouldRepriceOrder("refunded") {
		t.Fatal("only pending order should be repriced")
	}
	if !shouldRepriceOrder("pending") {
		t.Fatal("pending should be repriced")
	}
	if err := checkOrderIdentityChangeAllowed("partial", priceIdentityStandard, priceIdentityStaff); !errors.Is(err, ErrOrderPartiallyPaid) {
		t.Fatalf("partial identity change err = %v, want ErrOrderPartiallyPaid", err)
	}
	if err := checkOrderIdentityChangeAllowed("partial", priceIdentityStaff, priceIdentityStaff); err != nil {
		t.Fatalf("partial same identity err = %v, want nil", err)
	}
	if err := checkOrderIdentityChangeAllowed("paid", priceIdentityStandard, priceIdentityStaff); err != nil {
		t.Fatalf("paid guard err = %v, want nil (不重算)", err)
	}
}
