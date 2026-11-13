package his

import (
	"context"
	"errors"
	"testing"
)

// 收费链路 P0 集成测试：治疗自动收费 + 绑定材料、未结清禁执行、按项退费规则。

func TestTreatmentAutoChargeWithLinkedItems(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	parent, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "TRT100", Name: "中药熏蒸", Category: "治疗", Unit: "次", Price: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	material, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "MAT001", Name: "一次性治疗巾", Category: "其他", Unit: "张", Price: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	consumable, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "MAT002", Name: "药浴包", Category: "其他", Unit: "包", Price: 5.5,
	})
	if err != nil {
		t.Fatal(err)
	}

	links, err := svc.UpdateChargeItemLinks(ctx, 1, parent.ID, UpdateChargeItemLinksRequest{
		Links: []ChargeItemLinkInput{
			{ChildChargeItemID: material.ID, Quantity: 2},
			{ChildChargeItemID: consumable.ID, Quantity: 1},
		},
	})
	if err != nil || len(links) != 2 {
		t.Fatalf("update links: %v %+v", err, links)
	}
	if links[0].ChildName == "" || links[0].ChildPrice == 0 {
		t.Fatalf("link should carry child snapshot: %+v", links[0])
	}
	// 非自身 / 数量非正校验收敛到 400。
	if _, err := svc.UpdateChargeItemLinks(ctx, 1, parent.ID, UpdateChargeItemLinksRequest{
		Links: []ChargeItemLinkInput{{ChildChargeItemID: parent.ID, Quantity: 1}},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("self link err = %v", err)
	}

	encounterID, _ := walkinEncounter(t, svc, 1)
	order, err := svc.CreateTreatmentOrder(ctx, 1, 10, CreateTreatmentOrderRequest{
		EncounterID: encounterID, ChargeItemID: parent.ID, PlanTotalTimes: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.OrderID == nil {
		t.Fatal("treatment order should record charge order id")
	}

	chargeOrder, err := svc.GetOrder(ctx, 1, *order.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chargeOrder.Items) != 3 {
		t.Fatalf("order items = %d (%+v), want treatment + 2 materials", len(chargeOrder.Items), chargeOrder.Items)
	}
	if chargeOrder.ItemsTotal != 145.5 || chargeOrder.PayableTotal != 145.5 {
		t.Fatalf("order total = %v/%v, want 145.5", chargeOrder.ItemsTotal, chargeOrder.PayableTotal)
	}

	// 未结清禁止执行。
	if _, err := svc.ExecuteTreatment(ctx, 1, order.ID, 10, "未缴费"); !errors.Is(err, ErrTreatmentUnpaid) {
		t.Fatalf("unpaid execute err = %v", err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, chargeOrder.ID, CreatePaymentRequest{
		Method: "cash", Amount: chargeOrder.PayableTotal,
	}); err != nil {
		t.Fatal(err)
	}
	executed, err := svc.ExecuteTreatment(ctx, 1, order.ID, 10, "第一次")
	if err != nil || executed.CompletedTimes != 1 {
		t.Fatalf("execute: %v %+v", err, executed)
	}

	// 治疗已执行：治疗行不可退，也不能取消。
	if _, err := svc.RefundOrderItems(ctx, 1, 10, chargeOrder.ID, RefundOrderRequest{
		Reason: "测试", Items: []RefundItemInput{{OrderItemID: chargeOrder.Items[0].ID, Quantity: 1}},
	}); !errors.Is(err, ErrTreatmentExecutedCannotRefund) {
		t.Fatalf("refund executed treatment err = %v", err)
	}
	if _, err := svc.CancelTreatmentOrder(ctx, 1, order.ID); !errors.Is(err, ErrTreatmentExecuted) {
		t.Fatalf("cancel executed treatment err = %v", err)
	}
}

func TestRefundByItemRules(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	labItem, _ := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "LAB001", Name: "血常规", Category: "检验", Unit: "次", Price: 30,
	})
	examItem, _ := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "EXM001", Name: "腹部B超", Category: "检查", Unit: "次", Price: 80,
	})
	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "AMX900", Name: "阿莫西林", Spec: "0.25g*24", Unit: "盒", DrugType: "western", RetailPrice: 12.5,
	})
	if err != nil {
		t.Fatal(err)
	}

	encounterID, _ := walkinEncounter(t, svc, 1)
	if _, err := svc.CreateLabRequest(ctx, 1, 10, CreateLabRequestRequest{
		EncounterID: encounterID, ItemName: "血常规", SpecimenType: "静脉血", ChargeItemID: labItem.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateExamRequest(ctx, 1, 10, CreateExamRequestRequest{
		EncounterID: encounterID, ItemName: "腹部B超", BodyPart: "腹部", ChargeItemID: examItem.ID,
	}); err != nil {
		t.Fatal(err)
	}
	rx, err := svc.CreatePrescription(ctx, 1, CreatePrescriptionRequest{
		EncounterID: encounterID, Diagnosis: "感染",
		Items: []PrescriptionItemInput{{DrugID: drug.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	issued, err := svc.IssuePrescription(ctx, 1, rx.ID)
	if err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items: []OrderLineInput{
			{ItemType: orderItemRegistrationFee, Quantity: 1},
			{ItemType: orderItemDrug, RefID: drug.ID, Quantity: 1, PrescriptionID: &issued.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{
		Method: "cash", Amount: order.PayableTotal,
	}); err != nil {
		t.Fatal(err)
	}

	byRef := func(refID int64) OrderItem {
		for _, item := range order.Items {
			if item.RefID != nil && *item.RefID == refID && item.ItemType == orderItemChargeItem {
				return item
			}
		}
		t.Fatalf("order item for charge item %d not found in %+v", refID, order.Items)
		return OrderItem{}
	}
	examLine := byRef(examItem.ID)
	labLine := byRef(labItem.ID)
	var drugLine, feeLine OrderItem
	for _, item := range order.Items {
		switch item.ItemType {
		case orderItemDrug:
			drugLine = item
		case orderItemRegistrationFee:
			feeLine = item
		}
	}

	// 未执行的检查：按项退成功 → 检查申请置 cancelled，单仍 paid。
	refunded, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "患者取消检查", Items: []RefundItemInput{{OrderItemID: examLine.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if refunded.Status != "paid" || refunded.RefundedTotal != 80 {
		t.Fatalf("after exam refund status=%s refunded=%v, want paid/80", refunded.Status, refunded.RefundedTotal)
	}
	exams, err := svc.ListExamRequests(ctx, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(exams) == 0 || exams[0].Status != "cancelled" {
		t.Fatalf("exam status = %+v, want cancelled", exams)
	}

	// 检验已采样 → 拒绝；未采样时退费会把申请置 cancelled（这里先采样再验证拒绝）。
	labs, err := svc.ListLabRequests(ctx, 1, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(labs) == 0 {
		t.Fatal("lab request not found")
	}
	if _, err := svc.SampleLabRequest(ctx, 1, labs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "退检验", Items: []RefundItemInput{{OrderItemID: labLine.ID, Quantity: 1}},
	}); !errors.Is(err, ErrLabSampledCannotRefund) {
		t.Fatalf("sampled lab refund err = %v", err)
	}

	// 药品已发药 → 拒绝。
	if _, err := svc.InboundStock(ctx, 1, 10, InboundStockRequest{DrugID: drug.ID, Quantity: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DispensePrescription(ctx, 1, 10, rx.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "退药", Items: []RefundItemInput{{OrderItemID: drugLine.ID, Quantity: 1}},
	}); !errors.Is(err, ErrDispensedCannotRefund) {
		t.Fatalf("dispensed refund err = %v", err)
	}

	// 挂号费允许退（只记账，不联动挂号状态）。
	refunded, err = svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "退挂号费", Items: []RefundItemInput{{OrderItemID: feeLine.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if refunded.RefundedTotal != 90 {
		t.Fatalf("refunded total = %v, want 90", refunded.RefundedTotal)
	}
	if refunded.Status != "paid" {
		t.Fatalf("status = %s, want paid (lab/drug still on order)", refunded.Status)
	}
}

func TestRefundPartialQuantityThenFull(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "CURE01", Name: "换药", Category: "治疗", Unit: "次", Price: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: item.ID, Quantity: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PayableTotal != 90 {
		t.Fatalf("payable = %v, want 90", order.PayableTotal)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{
		Method: "cash", Amount: 90,
	}); err != nil {
		t.Fatal(err)
	}

	// 退 1 个 → 30 元，单仍 paid，可退数量剩 2。
	partial, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "少做一次", Items: []RefundItemInput{{OrderItemID: order.Items[0].ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if partial.Status != "paid" || partial.RefundedTotal != 30 {
		t.Fatalf("partial status=%s refunded=%v, want paid/30", partial.Status, partial.RefundedTotal)
	}
	if partial.Items[0].RefundedQuantity != 1 {
		t.Fatalf("refunded quantity = %v, want 1", partial.Items[0].RefundedQuantity)
	}
	// 超量拒绝。
	if _, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "超退", Items: []RefundItemInput{{OrderItemID: order.Items[0].ID, Quantity: 3}},
	}); !errors.Is(err, ErrRefundQuantityExceeded) {
		t.Fatalf("over refund err = %v", err)
	}

	// 退完剩余 2 个 → refunded，累计 90。
	full, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{
		Reason: "剩余全退", Items: []RefundItemInput{{OrderItemID: order.Items[0].ID, Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.Status != "refunded" || full.RefundedTotal != 90 || full.PaidTotal != 90 {
		t.Fatalf("full status=%s refunded=%v paid=%v, want refunded/90/90", full.Status, full.RefundedTotal, full.PaidTotal)
	}
}

func TestRefundRequiresPaidOrder(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "PEND01", Name: "雾化", Category: "治疗", Unit: "次", Price: 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: item.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{Reason: "pending"}); !errors.Is(err, ErrRefundOnlyPaid) {
		t.Fatalf("pending refund err = %v", err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RefundOrderItems(ctx, 1, 10, order.ID, RefundOrderRequest{Reason: "partial"}); !errors.Is(err, ErrRefundOnlyPaid) {
		t.Fatalf("partial refund err = %v", err)
	}
	// 旧整单接口同样只允许 paid（partial 走补收或先付清）。
	if _, err := svc.RefundOrder(ctx, 1, 10, order.ID, "partial"); !errors.Is(err, ErrRefundOnlyPaid) {
		t.Fatalf("legacy partial refund err = %v", err)
	}
}

func TestCreateOrderDiscountIsOverwrite(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "DISC01", Name: "理疗", Category: "治疗", Unit: "次", Price: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	first, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID:   encounterID,
		DiscountTotal: 10,
		Items:         []OrderLineInput{{ItemType: orderItemChargeItem, RefID: item.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 重复提交同一优惠：覆盖而非累加，应收仍 90。
	second, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID:   encounterID,
		DiscountTotal: 10,
		Items:         []OrderLineInput{{ItemType: orderItemChargeItem, RefID: item.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.DiscountTotal != 10 || second.PayableTotal != 90 {
		t.Fatalf("repeat discount = id %d->%d discount=%v payable=%v, want same/10/90",
			first.ID, second.ID, second.DiscountTotal, second.PayableTotal)
	}
	// 负数折扣拒绝。
	if _, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID:   encounterID,
		DiscountTotal: -1,
		Items:         []OrderLineInput{{ItemType: orderItemChargeItem, RefID: item.ID, Quantity: 1}},
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("negative discount err = %v", err)
	}
}

func TestCancelTreatmentReleasesOnlyItsChargeLine(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	treatment, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "TRT010", Name: "拔罐", Category: "治疗", Unit: "次", Price: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "OTH010", Name: "红外线", Category: "治疗", Unit: "次", Price: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, _ := walkinEncounter(t, svc, 1)
	treatmentOrder, err := svc.CreateTreatmentOrder(ctx, 1, 10, CreateTreatmentOrderRequest{
		EncounterID: encounterID, ChargeItemID: treatment.ID, PlanTotalTimes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: other.ID, Quantity: 1}},
	}); err != nil {
		t.Fatal(err)
	}

	cancelled, err := svc.CancelTreatmentOrder(ctx, 1, treatmentOrder.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancel treatment: %v %+v", err, cancelled)
	}
	chargeOrder, err := svc.GetOrder(ctx, 1, *treatmentOrder.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if len(chargeOrder.Items) != 1 || chargeOrder.Items[0].RefID == nil || *chargeOrder.Items[0].RefID != other.ID {
		t.Fatalf("remaining items = %+v, want only the other charge item", chargeOrder.Items)
	}
	if chargeOrder.PayableTotal != 20 {
		t.Fatalf("payable = %v, want 20", chargeOrder.PayableTotal)
	}
}
