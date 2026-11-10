package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

// giftCatalogFixture 是赠送测试用的价目表：2026 版带完整增量包规格，
// 2025 版只有 record 一个单位，用来验证"取最新 price_version"。
func giftCatalogFixture() []CatalogItem {
	return []CatalogItem{
		{
			ProductCode:    "tingxie",
			ProductName:    "听写病历",
			Edition:        EditionStandard,
			PriceVersion:   "2026",
			AnnualPrice:    Decimal("21600.00"),
			Quotas:         Quantities(`{"record":2500,"round":5000,"hour":400}`),
			IncrementPrice: Decimal("2500.00"),
			Increments:     Quantities(`{"record":250,"round":500,"hour":40}`),
		},
		{
			ProductCode:    "tingxie",
			ProductName:    "听写病历",
			Edition:        EditionStandard,
			PriceVersion:   "2025",
			AnnualPrice:    Decimal("20000.00"),
			IncrementPrice: Decimal("2000.00"),
			Increments:     Quantities(`{"record":250}`),
		},
		{
			ProductCode:    "yingke",
			ProductName:    "赢客专家",
			Edition:        EditionStandard,
			PriceVersion:   "2026",
			AnnualPrice:    Decimal("12600.00"),
			IncrementPrice: Decimal("1500.00"),
			Increments:     Quantities(`{"hour":80}`),
		},
		{
			// 没有增量包规格的产品：赠送既算不出包数也算不出等价金额。
			ProductCode:    "noinc",
			ProductName:    "无增量包产品",
			Edition:        EditionStandard,
			PriceVersion:   "2026",
			AnnualPrice:    Decimal("1000.00"),
			IncrementPrice: Decimal("0.00"),
			Increments:     Quantities(`{}`),
		},
	}
}

// ---------- E1 赠送预览 ----------

func TestGiftPreviewComputesEquivalentAmountAndNeedsApproval(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	service := NewService(repo)

	preview, err := service.PreviewGift(context.Background(), GiftPreviewRequest{
		TenantID:    9,
		ProductCode: " tingxie ",
		Edition:     " Standard ",
		Packs:       2,
	})
	if err != nil {
		t.Fatalf("PreviewGift returned error: %v", err)
	}
	if repo.catalogFilter != (CatalogFilter{ProductCode: "tingxie", Edition: EditionStandard}) {
		t.Fatalf("catalog filter not normalized: %+v", repo.catalogFilter)
	}
	if preview.ProductCode != "tingxie" || preview.ProductName != "听写病历" || preview.Edition != EditionStandard {
		t.Fatalf("unexpected preview header: %+v", preview)
	}
	if preview.Packs != 2 {
		t.Fatalf("packs = %d, want 2", preview.Packs)
	}
	if preview.PackPrice != Decimal("2500.00") {
		t.Fatalf("pack_price = %s, want 2500.00", preview.PackPrice)
	}
	if preview.EquivalentAmount != Decimal("5000.00") {
		t.Fatalf("equivalent_amount = %s, want 2 × 2500.00 = 5000.00", preview.EquivalentAmount)
	}
	if !preview.NeedsApproval {
		t.Fatal("2 packs must need approval")
	}
	// items 顺序按设计文档例子固定为 record、round、hour。
	gotOrder := make([]string, 0, len(preview.Items))
	for _, item := range preview.Items {
		gotOrder = append(gotOrder, item.Unit)
	}
	if len(gotOrder) != 3 || gotOrder[0] != "record" || gotOrder[1] != "round" || gotOrder[2] != "hour" {
		t.Fatalf("items order = %v, want [record round hour]", gotOrder)
	}

	// 单包规格 × 2：record 250×2、round 500×2、hour 40×2。
	quotas := make(map[string]Decimal, len(preview.Items))
	for _, item := range preview.Items {
		quotas[item.Unit] = item.Quotas
	}
	want := map[string]Decimal{"record": "500", "round": "1000", "hour": "80"}
	if len(quotas) != len(want) {
		t.Fatalf("items = %+v, want %v", preview.Items, want)
	}
	for unit, expected := range want {
		if quotas[unit] != expected {
			t.Fatalf("items.%s = %s, want %s", unit, quotas[unit], expected)
		}
	}
}

func TestGiftPreviewOnePackNeedsNoApproval(t *testing.T) {
	service := NewService(&fakeRepository{catalogItems: giftCatalogFixture()})

	preview, err := service.PreviewGift(context.Background(), GiftPreviewRequest{
		ProductCode: "tingxie",
		Edition:     EditionStandard,
		Packs:       1,
	})
	if err != nil {
		t.Fatalf("PreviewGift returned error: %v", err)
	}
	if preview.NeedsApproval {
		t.Fatal("1 pack must not need approval")
	}
	if preview.PackPrice != Decimal("2500.00") || preview.EquivalentAmount != Decimal("2500.00") {
		t.Fatalf("unexpected amounts: pack=%s equivalent=%s", preview.PackPrice, preview.EquivalentAmount)
	}
	quotas := make(map[string]Decimal, len(preview.Items))
	for _, item := range preview.Items {
		quotas[item.Unit] = item.Quotas
	}
	if quotas["record"] != Decimal("250") || quotas["round"] != Decimal("500") || quotas["hour"] != Decimal("40") {
		t.Fatalf("unexpected single-pack quotas: %+v", preview.Items)
	}
}

func TestGiftPreviewUsesLatestPriceVersionOnly(t *testing.T) {
	service := NewService(&fakeRepository{catalogItems: giftCatalogFixture()})

	// 2025 版只有 record 一个单位且单价 2000；命中 2026 版才不会报错、金额才是 2500。
	preview, err := service.PreviewGift(context.Background(), GiftPreviewRequest{
		ProductCode: "tingxie",
		Packs:       1,
	})
	if err != nil {
		t.Fatalf("PreviewGift returned error: %v", err)
	}
	if preview.PackPrice != Decimal("2500.00") || len(preview.Items) != 3 {
		t.Fatalf("expected latest (2026) price version, got %+v", preview)
	}
}

func TestGiftPreviewValidatesInput(t *testing.T) {
	cases := []struct {
		name string
		req  GiftPreviewRequest
	}{
		{"missing product_code", GiftPreviewRequest{Packs: 1}},
		{"invalid edition", GiftPreviewRequest{ProductCode: "tingxie", Edition: "enterprise", Packs: 1}},
		{"zero packs", GiftPreviewRequest{ProductCode: "tingxie", Packs: 0}},
		{"negative packs", GiftPreviewRequest{ProductCode: "tingxie", Packs: -2}},
		{"too many packs", GiftPreviewRequest{ProductCode: "tingxie", Packs: maxGiftPacks + 1}},
		{"product without increments", GiftPreviewRequest{ProductCode: "noinc", Packs: 1}},
		{"unknown product", GiftPreviewRequest{ProductCode: "unknown", Packs: 1}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewService(&fakeRepository{catalogItems: giftCatalogFixture()})
			_, err := service.PreviewGift(context.Background(), testCase.req)
			assertValidationError(t, err)
		})
	}
}

// ---------- E1 赠送录单强校验 ----------

func TestCreateGiftOrderOnePackWithoutApproval(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	service := newOrderTestService(repo)

	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeGift,
		Reason:    "送 1 个听写增量包",
		Items: []CreateOrderItem{{
			ProductCode: "tingxie",
			Edition:     EditionStandard,
			Quotas:      QuantityInputs{"record": Decimal("250"), "round": Decimal("500"), "hour": Decimal("40")},
		}},
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if order.TotalAmount != Decimal("0.00") || order.PayableAmount != Decimal("0.00") {
		t.Fatalf("gift amounts must stay 0, got %+v", order)
	}
	if len(order.Items) != 1 || order.Items[0].Price != Decimal("2500.00") {
		t.Fatalf("gift item price must be 1 × increment_price = 2500.00, got %+v", order.Items)
	}
}

func TestCreateGiftOrderOverOnePackRequiresApproval(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	service := newOrderTestService(repo)
	twoPackQuotas := QuantityInputs{"record": Decimal("500"), "round": Decimal("1000"), "hour": Decimal("80")}

	_, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeGift,
		Reason:    "送 2 个听写增量包",
		Items:     []CreateOrderItem{{ProductCode: "tingxie", Edition: EditionStandard, Quotas: twoPackQuotas}},
	})
	if !errors.Is(err, ErrGiftApprovalRequired) {
		t.Fatalf("expected ErrGiftApprovalRequired, got %v", err)
	}
	if repo.insertOrderCalls != 0 {
		t.Fatalf("rejected gift must not be inserted, calls=%d", repo.insertOrderCalls)
	}

	// 记了 approved_by 才能建单，且行金额 = 2 × increment_price。
	approver := int64(77)
	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:   9,
		OrderType:  OrderTypeGift,
		Reason:     "老板同意送 2 个听写增量包",
		ApprovedBy: &approver,
		Items:      []CreateOrderItem{{ProductCode: "tingxie", Edition: EditionStandard, Quotas: twoPackQuotas}},
	})
	if err != nil {
		t.Fatalf("approved gift returned error: %v", err)
	}
	if order.Items[0].Price != Decimal("5000.00") {
		t.Fatalf("gift item price = %s, want 5000.00", order.Items[0].Price)
	}
	if repo.insertOrderInput.ApprovedBy == nil || *repo.insertOrderInput.ApprovedBy != 77 {
		t.Fatalf("approved_by not persisted: %+v", repo.insertOrderInput.ApprovedBy)
	}
	if order.TotalAmount != Decimal("0.00") || order.PayableAmount != Decimal("0.00") {
		t.Fatalf("gift total must stay 0, got %+v", order)
	}
}

func TestCreateGiftOrderDerivesPacksFromQuotas(t *testing.T) {
	approver := int64(77)
	cases := []struct {
		name    string
		quotas  QuantityInputs
		packs   string // 期望写入订单的行金额
		approve bool
	}{
		{"one pack", QuantityInputs{"record": Decimal("250")}, "2500.00", false},
		{"two packs", QuantityInputs{"record": Decimal("500")}, "5000.00", true},
		// 单位之间包数不一致：record 2 包、round 1 包。
		{"inconsistent units", QuantityInputs{"record": Decimal("500"), "round": Decimal("500")}, "", false},
		// 不是增量包规格的整数倍。
		{"non multiple", QuantityInputs{"record": Decimal("300")}, "", false},
		// 单位不在增量包规格里。
		{"unit not in increments", QuantityInputs{"shot": Decimal("10")}, "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			repo := &fakeRepository{catalogItems: giftCatalogFixture()}
			service := newOrderTestService(repo)
			req := CreateOrderRequest{
				TenantID:  9,
				OrderType: OrderTypeGift,
				Reason:    "赠送测试",
				Items:     []CreateOrderItem{{ProductCode: "tingxie", Edition: EditionStandard, Quotas: testCase.quotas}},
			}
			if testCase.approve {
				req.ApprovedBy = &approver
			}
			order, err := service.CreateOrder(context.Background(), req)
			if testCase.packs == "" {
				assertOrderItemsError(t, err)
				return
			}
			if err != nil {
				t.Fatalf("CreateOrder returned error: %v", err)
			}
			if order.Items[0].Price != Decimal(testCase.packs) {
				t.Fatalf("item price = %s, want %s", order.Items[0].Price, testCase.packs)
			}
		})
	}
}

func TestCreateGiftOrderRequiresCatalogEntry(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	service := newOrderTestService(repo)

	_, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeGift,
		Reason:    "赠送测试",
		Items: []CreateOrderItem{{
			ProductCode: "unknown",
			Edition:     EditionStandard,
			Quotas:      QuantityInputs{"record": Decimal("250")},
		}},
	})
	assertOrderItemsError(t, err)
	if repo.insertOrderCalls != 0 {
		t.Fatalf("unknown product must not be inserted, calls=%d", repo.insertOrderCalls)
	}
}

func TestCreateGiftOrderOverridesManualPrice(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	service := newOrderTestService(repo)

	// 运营手填 1 元也必须按价目表改写成"相当于 2500 元"的等价金额。
	manualPrice := Decimal("1.00")
	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeGift,
		Reason:    "赠送测试",
		Items: []CreateOrderItem{{
			ProductCode: "tingxie",
			Edition:     EditionStandard,
			Quotas:      QuantityInputs{"record": Decimal("250")},
			Price:       &manualPrice,
		}},
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if order.Items[0].Price != Decimal("2500.00") {
		t.Fatalf("gift item price = %s, want catalog equivalent 2500.00", order.Items[0].Price)
	}
}

// ---------- E1 handler ----------

func TestGiftPreviewHandler(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/gift/preview", adminToken,
		`{"tenant_id":9,"product_code":"tingxie","edition":"standard","packs":2}`)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body=%s", response.Code, response.Body.String())
	}
	var preview GiftPreviewResponse
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatalf("decode preview: %v (%s)", err, response.Body.String())
	}
	if preview.ProductName != "听写病历" || preview.Packs != 2 ||
		preview.PackPrice != Decimal("2500.00") || preview.EquivalentAmount != Decimal("5000.00") ||
		!preview.NeedsApproval || len(preview.Items) != 3 {
		t.Fatalf("unexpected preview payload: %+v", preview)
	}

	if response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders/gift/preview", adminToken, `{"packs":0}`); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid packs should be 400, got %d body=%s", response.Code, response.Body.String())
	}
}

func TestCreateGiftOrderHandlerApprovalCode(t *testing.T) {
	repo := &fakeRepository{catalogItems: giftCatalogFixture()}
	mux := newOrderTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders", adminToken,
		`{"tenant_id":9,"order_type":"gift","reason":"送 2 个包",
		  "items":[{"product_code":"tingxie","edition":"standard","quotas":{"record":500,"round":1000}}]}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unapproved gift status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Code != "GIFT_APPROVAL_REQUIRED" {
		t.Fatalf("code = %q, want GIFT_APPROVAL_REQUIRED", payload.Code)
	}
	if payload.Message != "赠送超过 1 个增量包，需要老板审批" {
		t.Fatalf("message = %q", payload.Message)
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/orders", adminToken,
		`{"tenant_id":9,"order_type":"gift","reason":"送 1 个包",
		  "items":[{"product_code":"tingxie","edition":"standard","quotas":{"record":250,"round":500}}]}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("one-pack gift status = %d, body=%s", response.Code, response.Body.String())
	}
}
