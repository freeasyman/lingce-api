package his

import (
	"context"
	"errors"
	"testing"
)

// 拼音：create/update/import 写入后，搜索接口可按全拼或首字母命中。
func TestPinyinWrittenOnCreateUpdateAndImport(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	item, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "PY001", Name: "测试拼音项目", Category: "其他", Unit: "次", Price: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, keyword := range []string{"cspyxm", "ceshipinyinxiangmu"} {
		page, err := svc.SearchChargeItems(ctx, 1, keyword, "", 1, 50)
		if err != nil {
			t.Fatal(err)
		}
		if !chargeItemPageContains(page, item.ID) {
			t.Fatalf("search %q should hit charge item %d, got %+v", keyword, item.ID, page.Items)
		}
	}

	renamed, err := svc.UpdateChargeItem(ctx, 1, item.ID, UpdateChargeItemRequest{Name: strPtr("血常规")})
	if err != nil {
		t.Fatal(err)
	}
	page, err := svc.SearchChargeItems(ctx, 1, "xcg", "", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !chargeItemPageContains(page, renamed.ID) {
		t.Fatalf("search xcg should hit renamed item, got %+v", page.Items)
	}

	// 导入路径直接落库，也必须带拼音。
	if err := svc.store.InsertImportChargeItem(ctx, 1, importChargeItemValues{
		Code: "PY002", Name: "妇科B超", Category: "检查", Unit: "次", Price: 80, Status: "active",
	}); err != nil {
		t.Fatal(err)
	}
	page, err = svc.SearchChargeItems(ctx, 1, "fkb", "", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].Code != "PY002" {
		t.Fatalf("search fkb import item = %+v", page)
	}

	drug, err := svc.CreateDrug(ctx, 1, CreateDrugRequest{
		Code: "PYD001", Name: "甲硝唑片", Spec: "0.2g*21片", Unit: "盒",
		DrugType: "western", RetailPrice: 6.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	drugs, err := svc.ListDrugs(ctx, 1, "jxzp")
	if err != nil {
		t.Fatal(err)
	}
	foundDrug := false
	for _, candidate := range drugs {
		if candidate.ID == drug.ID {
			foundDrug = true
		}
	}
	if !foundDrug {
		t.Fatalf("search jxzp should hit drug %d, got %+v", drug.ID, drugs)
	}
	// 编辑药品（改价）不覆盖拼音，仍可搜到。
	if _, err := svc.UpdateDrug(ctx, 1, drug.ID, UpdateDrugRequest{RetailPrice: floatPtr(7.5)}); err != nil {
		t.Fatal(err)
	}
	drugs, err = svc.ListDrugs(ctx, 1, "jiaxiaozuopian")
	if err != nil || len(drugs) != 1 {
		t.Fatalf("search full pinyin after update: %v %+v", err, drugs)
	}
}

func chargeItemPageContains(page ChargeItemPage, id int64) bool {
	for _, item := range page.Items {
		if item.ID == id {
			return true
		}
	}
	return false
}

// pending 单收款时按当前身份重算：普通 105 → 员工 93，行 rate 正确并留痕。
func TestRepricePendingOrderOnPayment(t *testing.T) {
	svc, pool := newTestService(t)
	ctx := context.Background()

	lab, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "RPL01", Name: "血常规", Category: "检验", Unit: "次", Price: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	exam, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "RPE01", Name: "妇科B超", Category: "检查", Unit: "次", Price: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, patient := walkinEncounter(t, svc, 1)
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items: []OrderLineInput{
			{ItemType: orderItemChargeItem, RefID: lab.ID, Quantity: 1},
			{ItemType: orderItemChargeItem, RefID: exam.ID, Quantity: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if order.PayableTotal != 105 || order.PriceIdentity != priceIdentityStandard {
		t.Fatalf("standard order = %v/%s, want 105/standard", order.PayableTotal, order.PriceIdentity)
	}

	if err := svc.SetPatientPriceIdentity(ctx, 1, patient.CustomerID, 10, priceIdentityStaff); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityStaff, map[string]float64{
		discountCategoryExam: 0.85, discountCategoryLab: 1,
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := svc.PreviewOrderReprice(ctx, 1, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.Repriced || preview.PreviousPayable != 105 || preview.PayableTotal != 93 {
		t.Fatalf("preview = %+v, want repriced 105 -> 93", preview)
	}

	paid, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 93})
	if err != nil {
		t.Fatal(err)
	}
	if !paid.Repriced || paid.PreviousPayable == nil || *paid.PreviousPayable != 105 {
		t.Fatalf("paid repriced/previous = %v/%v, want true/105", paid.Repriced, paid.PreviousPayable)
	}
	if paid.PayableTotal != 93 || paid.PaidTotal != 93 || paid.Status != "paid" {
		t.Fatalf("paid totals = %v/%v/%s, want 93/93/paid", paid.PayableTotal, paid.PaidTotal, paid.Status)
	}
	for _, item := range paid.Items {
		switch item.Category {
		case discountCategoryLab:
			if item.IdentityRate != 1 || item.Payable != 25 {
				t.Fatalf("lab line = %+v, want rate1/payable25", item)
			}
		case discountCategoryExam:
			if item.IdentityRate != 0.85 || item.Payable != 68 {
				t.Fatalf("exam line = %+v, want rate.85/payable68", item)
			}
		}
	}
	var logCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM his_order_repricing_logs
		WHERE tenant_id = 1 AND order_id = $1`, order.ID).Scan(&logCount); err != nil {
		t.Fatal(err)
	}
	if logCount != 1 {
		t.Fatalf("repricing logs = %d, want 1", logCount)
	}

	// 已收费后再次收款：不重算，直接拒绝。
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 1}); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("paid re-charge err = %v, want ErrInvalidState", err)
	}
	preview, err = svc.PreviewOrderReprice(ctx, 1, order.ID)
	if err != nil || preview.Repriced {
		t.Fatalf("paid preview = %+v err=%v, want not repriced", preview, err)
	}
}

// partial 单身份变更后拒绝补收；身份未变仍可正常补收。
func TestPartialOrderIdentityChangeRejected(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	exam, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "RPX01", Name: "胸部CT", Category: "检查", Unit: "次", Price: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, patient := walkinEncounter(t, svc, 1)
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: exam.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 10})
	if err != nil || partial.Status != "partial" {
		t.Fatalf("partial payment = %+v err=%v", partial, err)
	}

	if err := svc.SetPatientPriceIdentity(ctx, 1, patient.CustomerID, 10, priceIdentityStaff); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityStaff, map[string]float64{discountCategoryExam: 0.85}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 90}); !errors.Is(err, ErrOrderPartiallyPaid) {
		t.Fatalf("partial identity change err = %v, want ErrOrderPartiallyPaid", err)
	}
	if _, err := svc.PreviewOrderReprice(ctx, 1, order.ID); !errors.Is(err, ErrOrderPartiallyPaid) {
		t.Fatalf("partial preview err = %v, want ErrOrderPartiallyPaid", err)
	}

	// 身份改回下单快照后可继续补收，且不重算。
	if err := svc.SetPatientPriceIdentity(ctx, 1, patient.CustomerID, 10, priceIdentityStandard); err != nil {
		t.Fatal(err)
	}
	paid, err := svc.AddPayment(ctx, 1, 10, order.ID, CreatePaymentRequest{Method: "cash", Amount: 90})
	if err != nil {
		t.Fatal(err)
	}
	if paid.Repriced || paid.Status != "paid" || paid.PayableTotal != 100 {
		t.Fatalf("partial settle = %+v, want paid/100/not repriced", paid)
	}
}

// 合并下单（CreateOrder 命中未结单）也按当前身份重算已有行。
func TestCreateOrderMergeRepricesPendingRows(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	lab, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "MGL01", Name: "尿常规", Category: "检验", Unit: "次", Price: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	exam, err := svc.CreateChargeItem(ctx, 1, CreateChargeItemRequest{
		Code: "MGE01", Name: "腹部彩超", Category: "检查", Unit: "次", Price: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	encounterID, patient := walkinEncounter(t, svc, 1)
	order, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: lab.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetPatientPriceIdentity(ctx, 1, patient.CustomerID, 10, priceIdentityStaff); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateDiscountRules(ctx, 1, priceIdentityStaff, map[string]float64{discountCategoryExam: 0.85}); err != nil {
		t.Fatal(err)
	}
	merged, err := svc.CreateOrder(ctx, 1, 10, CreateOrderRequest{
		EncounterID: encounterID,
		Items:       []OrderLineInput{{ItemType: orderItemChargeItem, RefID: exam.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != order.ID || !merged.Repriced || merged.PayableTotal != 93 {
		t.Fatalf("merged = id %d repriced=%v payable=%v, want %d/true/93", merged.ID, merged.Repriced, merged.PayableTotal, order.ID)
	}
	for _, item := range merged.Items {
		if item.Category == discountCategoryExam && (item.IdentityRate != 0.85 || item.Payable != 68) {
			t.Fatalf("merged exam line = %+v, want rate.85/payable68", item)
		}
	}
}
