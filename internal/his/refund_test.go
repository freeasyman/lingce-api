package his

import (
	"errors"
	"testing"
)

// 按项退费的纯逻辑测试：数量校验、金额按 payable 比例、分次退费收口、整单默认全退。

func refundLineFixture(id, quantity, payable, refundedQuantity, refundedAmount float64) refundOrderLine {
	return refundOrderLine{
		ID:               int64(id),
		ItemType:         orderItemChargeItem,
		Quantity:         quantity,
		Payable:          payable,
		RefundedQuantity: refundedQuantity,
		RefundedAmount:   refundedAmount,
	}
}

func TestRefundLineAmountProportional(t *testing.T) {
	// 数量 3、行应付 100：退 1 个 = 33.33，退剩余 2 个 = 66.67（收口，累计正好 100）。
	amount, err := refundLineAmount(100, 3, 0, 0, 1)
	if err != nil || amount != 33.33 {
		t.Fatalf("partial amount = %v, %v; want 33.33", amount, err)
	}
	amount, err = refundLineAmount(100, 3, 1, 33.33, 2)
	if err != nil || amount != 66.67 {
		t.Fatalf("remaining amount = %v, %v; want 66.67", amount, err)
	}
	// 全额退（一次退完）= payable 本身。
	amount, err = refundLineAmount(88.8, 1, 0, 0, 1)
	if err != nil || amount != 88.8 {
		t.Fatalf("full amount = %v, %v; want 88.8", amount, err)
	}
}

func TestRefundLineAmountRejectsOverRefund(t *testing.T) {
	for _, tt := range []struct {
		name                                              string
		payable, quantity, refundedQty, amount, refundQty float64
	}{
		{"over remaining", 100, 3, 1, 33.33, 3},
		{"zero quantity", 100, 0, 0, 0, 1},
		{"zero refund", 100, 3, 0, 0, 0},
		{"negative refunded", 100, 3, -1, 0, 1},
	} {
		if _, err := refundLineAmount(tt.payable, tt.quantity, tt.refundedQty, tt.amount, tt.refundQty); !errors.Is(err, ErrInvalidRequest) && !errors.Is(err, ErrRefundQuantityExceeded) {
			t.Fatalf("%s: err = %v, want invalid/exceeded", tt.name, err)
		}
	}
}

func TestBuildRefundTargetsDefaultsToAllRemaining(t *testing.T) {
	lines := []refundOrderLine{
		refundLineFixture(1, 2, 40, 0, 0),
		refundLineFixture(2, 1, 10, 1, 10), // already fully refunded
		refundLineFixture(3, 3, 30, 1, 10),
	}
	targets, err := buildRefundTargets(lines, nil)
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(targets))
	}
	if targets[0].Quantity != 2 || targets[1].Quantity != 2 {
		t.Fatalf("target quantities = %v/%v, want 2/2", targets[0].Quantity, targets[1].Quantity)
	}
}

func TestBuildRefundTargetsValidatesItems(t *testing.T) {
	lines := []refundOrderLine{refundLineFixture(1, 2, 40, 0, 0)}

	if _, err := buildRefundTargets(lines, []RefundItemInput{{OrderItemID: 1, Quantity: 3}}); !errors.Is(err, ErrRefundQuantityExceeded) {
		t.Fatalf("over qty err = %v", err)
	}
	if _, err := buildRefundTargets(lines, []RefundItemInput{{OrderItemID: 999, Quantity: 1}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("foreign item err = %v", err)
	}
	if _, err := buildRefundTargets(lines, []RefundItemInput{{OrderItemID: 1, Quantity: 1}, {OrderItemID: 1, Quantity: 1}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("duplicate item err = %v", err)
	}
	if _, err := buildRefundTargets(lines, []RefundItemInput{{OrderItemID: 1, Quantity: 0}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero qty err = %v", err)
	}
}

func TestCheckRefundLineAllowed(t *testing.T) {
	prescriptionID := int64(7)
	chargeItemID := int64(9)
	restrictions := refundRestrictions{
		prescriptionStatus: map[int64]string{7: "dispensed"},
		labStatus:          map[int64][]string{9: {"sampled"}},
		examStatus:         map[int64][]string{9: {"in_progress"}},
		treatmentCompleted: map[int64]int{9: 1},
	}

	drug := refundOrderLine{ItemType: orderItemDrug, PrescriptionID: &prescriptionID}
	if err := checkRefundLineAllowed(drug, restrictions); !errors.Is(err, ErrDispensedCannotRefund) {
		t.Fatalf("dispensed drug err = %v", err)
	}
	lab := refundOrderLine{ItemType: orderItemChargeItem, RefID: &chargeItemID}
	if err := checkRefundLineAllowed(lab, restrictions); !errors.Is(err, ErrLabSampledCannotRefund) {
		t.Fatalf("sampled lab err = %v", err)
	}

	restrictions.labStatus = map[int64][]string{9: {"pending"}}
	restrictions.examStatus = map[int64][]string{9: {"in_progress"}}
	if err := checkRefundLineAllowed(lab, restrictions); !errors.Is(err, ErrExamStartedCannotRefund) {
		t.Fatalf("started exam err = %v", err)
	}

	restrictions.examStatus = map[int64][]string{9: {"pending"}}
	if err := checkRefundLineAllowed(lab, restrictions); !errors.Is(err, ErrTreatmentExecutedCannotRefund) {
		t.Fatalf("executed treatment err = %v", err)
	}

	restrictions.treatmentCompleted = map[int64]int{}
	if err := checkRefundLineAllowed(refundOrderLine{ItemType: orderItemRegistrationFee}, restrictions); err != nil {
		t.Fatalf("registration fee should be refundable: %v", err)
	}
	if err := checkRefundLineAllowed(lab, restrictions); err != nil {
		t.Fatalf("unexecuted lab should be refundable: %v", err)
	}
}
