package billing

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// fakeRepository 是 Repository 的内存实现，让 service 的单测不依赖真实数据库。
type fakeRepository struct {
	catalogFilter CatalogFilter
	catalogItems  []CatalogItem
	catalogCalls  int
	catalogErr    error

	insertInput CatalogVersionInput
	insertItem  CatalogItem
	insertCalls int
	insertErr   error

	entitlementTenantID    int64
	entitlementProduct     string
	entitlementIncludeZero bool
	entitlementItems       []Entitlement
	entitlementCalls       int
	entitlementErr         error

	servicePeriodTenantID int64
	servicePeriod         *ServicePeriod
	servicePeriodErr      error

	ledgerQuery LedgerQuery
	ledgerItems []LedgerEntry
	ledgerTotal int64
	ledgerCalls int
	ledgerErr   error

	// 阶段 F：流水 CSV 导出。exportRows 模拟 entitlement_ledger JOIN product_catalog 的结果；
	// exportFillLimit 让 fake 直接返回 limit 行，用来触发"超过上限"的 400。
	exportRows      []LedgerExportRow
	exportErr       error
	exportFillLimit bool
	exportCalls     int
	exportQuery     LedgerQuery
	exportLimit     int

	// 阶段 C：订单状态。orders 模拟 billing_orders 表，方法按真实 store 的约束改它，
	// 这样"激活一次后不能再激活/退款"这类规则在 service 单测里也能被验证。
	orders            map[int64]Order
	nextOrderID       int64
	insertOrderInput  CreateOrderInput
	insertOrderInputs []CreateOrderInput
	insertOrderCalls  int
	insertOrderErrs   []error
	listOrdersQuery   OrderQuery
	listOrdersItems   []Order
	listOrdersTotal   int64
	listOrdersErr     error
	getOrderErr       error
	settleInput       SettleOrderInput
	settleCalls       int
	settleErr         error
	activateInput     ActivateOrderInput
	activateInputs    []ActivateOrderInput
	activateCalls     int
	activateErr       error
	activationPeriod  ServicePeriod
	refundInput       RefundOrderInput
	refundCalls       int
	refundErr         error
	adminName         string
	adminNameID       int64
	adminNameErr      error
	grantCalls        int
	grantedLines      []GrantLine

	// 阶段 D：扣量。deductRows/deductKeys 模拟 product_entitlements 与
	// entitlement_ledger 的部分唯一索引；usageLedger 保存已提交的 usage 流水，
	// 让"同键第二次不重复扣"能在内存里断言。
	deductTenantID   int64
	deductRows       []Entitlement
	deductKeys       map[string]struct{}
	usageLedger      []DeductLedgerEntry
	beginDeductCalls int
	applyCalls       int
	recomputeCalls   int
	commitCalls      int
	rollbackCalls    int

	// 阶段 E：续费结转与到期处理。subscriptions 模拟 tenant_subscriptions，
	// entitlementsByTenant 模拟按租户分组的 product_entitlements；
	// renewLedger/expireLedger 记录写出的流水，供"流水与余额一致"的断言。
	subscriptions        map[int64]fakeSubscription
	entitlementsByTenant map[int64][]Entitlement
	renewInput           RenewOrderInput
	renewCalls           int
	renewErr             error
	renewLedger          []fakeLedgerWrite
	expireListCalls      int
	expireCalls          []int64
	expireErr            error
	expireLedger         []fakeLedgerWrite
}

func (f *fakeRepository) ListCatalog(_ context.Context, filter CatalogFilter) ([]CatalogItem, error) {
	f.catalogCalls++
	f.catalogFilter = filter
	return f.catalogItems, f.catalogErr
}

func (f *fakeRepository) InsertCatalogVersion(_ context.Context, input CatalogVersionInput) (CatalogItem, error) {
	f.insertCalls++
	f.insertInput = input
	if f.insertErr != nil {
		return CatalogItem{}, f.insertErr
	}
	return f.insertItem, nil
}

func (f *fakeRepository) ListEntitlements(_ context.Context, tenantID int64, product string, includeZero bool) ([]Entitlement, error) {
	f.entitlementCalls++
	f.entitlementTenantID = tenantID
	f.entitlementProduct = product
	f.entitlementIncludeZero = includeZero
	if rows, ok := f.entitlementsByTenant[tenantID]; ok {
		return cloneEntitlements(rows), f.entitlementErr
	}
	return f.entitlementItems, f.entitlementErr
}

func (f *fakeRepository) GetServicePeriod(_ context.Context, tenantID int64) (*ServicePeriod, error) {
	f.servicePeriodTenantID = tenantID
	return f.servicePeriod, f.servicePeriodErr
}

func (f *fakeRepository) ListLedger(_ context.Context, query LedgerQuery) ([]LedgerEntry, int64, error) {
	f.ledgerCalls++
	f.ledgerQuery = query
	return f.ledgerItems, f.ledgerTotal, f.ledgerErr
}

func TestListCatalogKeepsLatestPriceVersionPerProductEdition(t *testing.T) {
	repo := &fakeRepository{catalogItems: []CatalogItem{
		{ProductCode: "yingke", ProductName: "赢客专家", Edition: EditionStandard, PriceVersion: "2026"},
		{ProductCode: "yingke", ProductName: "赢客专家", Edition: EditionStandard, PriceVersion: "2027"},
		{ProductCode: "yingke", ProductName: "赢客专家", Edition: EditionTrial, PriceVersion: "2026"},
		{ProductCode: "kehu", ProductName: "客户总监", Edition: EditionStandard, PriceVersion: "9"},
		{ProductCode: "kehu", ProductName: "客户总监", Edition: EditionStandard, PriceVersion: "10"},
	}}
	service := NewService(repo)

	items, err := service.ListCatalog(context.Background(), CatalogFilter{
		ProductCode:  " yingke ",
		Edition:      " Standard ",
		PriceVersion: " 2026 ",
	})
	if err != nil {
		t.Fatalf("ListCatalog returned error: %v", err)
	}
	if repo.catalogFilter != (CatalogFilter{ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026"}) {
		t.Fatalf("filter not normalized: %+v", repo.catalogFilter)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 product+edition rows, got %d: %+v", len(items), items)
	}
	selected := make(map[string]string, len(items))
	for _, item := range items {
		selected[item.ProductCode+"/"+item.Edition] = item.PriceVersion
	}
	if selected["yingke/standard"] != "2027" {
		t.Fatalf("yingke standard should keep latest version 2027, got %q", selected["yingke/standard"])
	}
	if selected["yingke/trial"] != "2026" {
		t.Fatalf("yingke trial should keep 2026, got %q", selected["yingke/trial"])
	}
	if selected["kehu/standard"] != "10" {
		t.Fatalf("numeric price versions must compare by value (10 > 9), got %q", selected["kehu/standard"])
	}
}

func TestListCatalogRejectsUnknownEditionWithoutTouchingStore(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)

	_, err := service.ListCatalog(context.Background(), CatalogFilter{Edition: "enterprise"})
	assertValidationError(t, err)
	if repo.catalogCalls != 0 {
		t.Fatalf("store must not be called for invalid edition, calls=%d", repo.catalogCalls)
	}
}

func TestListCatalogReturnsEmptySliceInsteadOfNil(t *testing.T) {
	service := NewService(&fakeRepository{})

	items, err := service.ListCatalog(context.Background(), CatalogFilter{})
	if err != nil {
		t.Fatalf("ListCatalog returned error: %v", err)
	}
	if items == nil {
		t.Fatal("expected empty slice so JSON is [] instead of null")
	}
	if len(items) != 0 {
		t.Fatalf("expected no items, got %d", len(items))
	}
}

func TestCreateCatalogVersionValidatesInput(t *testing.T) {
	longCode := strings.Repeat("x", 33)
	cases := []struct {
		name string
		req  CreateCatalogRequest
	}{
		{"missing product_code", CreateCatalogRequest{Edition: EditionStandard, PriceVersion: "2026"}},
		{"invalid edition", CreateCatalogRequest{ProductCode: "yingke", Edition: "enterprise", PriceVersion: "2026"}},
		{"missing price_version", CreateCatalogRequest{ProductCode: "yingke", Edition: EditionStandard}},
		{"unsupported quota unit", CreateCatalogRequest{
			ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
			Quotas: QuantityInputs{"minute": Decimal("100")},
		}},
		{"unsupported increment unit", CreateCatalogRequest{
			ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
			Increments: QuantityInputs{"包": Decimal("1")},
		}},
		{"negative annual_price", CreateCatalogRequest{
			ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
			AnnualPrice: Decimal("-1"),
		}},
		{"non numeric increment_price", CreateCatalogRequest{
			ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
			IncrementPrice: Decimal("abc"),
		}},
		{"negative quota", CreateCatalogRequest{
			ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
			Quotas: QuantityInputs{"hour": Decimal("-5")},
		}},
		{"duplicate unit after trimming", CreateCatalogRequest{
			ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
			Quotas: QuantityInputs{"hour": Decimal("1"), " hour": Decimal("2")},
		}},
		{"product_code too long", CreateCatalogRequest{
			ProductCode: longCode, Edition: EditionStandard, PriceVersion: "2026",
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepository{}
			service := NewService(repo)
			_, err := service.CreateCatalogVersion(context.Background(), test.req)
			assertValidationError(t, err)
			if repo.insertCalls != 0 {
				t.Fatalf("store must not be called for invalid input, calls=%d", repo.insertCalls)
			}
		})
	}
}

func TestCreateCatalogVersionNormalizesPayload(t *testing.T) {
	repo := &fakeRepository{insertItem: CatalogItem{ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2027"}}
	service := NewService(repo)

	item, err := service.CreateCatalogVersion(context.Background(), CreateCatalogRequest{
		ProductCode:    " yingke ",
		ProductName:    " 赢客专家 ",
		Edition:        " Standard ",
		PriceVersion:   " 2027 ",
		AnnualPrice:    Decimal("13200.50"),
		Quotas:         QuantityInputs{"hour": Decimal("500"), "record": Decimal("10.5")},
		IncrementPrice: Decimal("1600"),
		Increments:     QuantityInputs{"hour": Decimal("90")},
	})
	if err != nil {
		t.Fatalf("CreateCatalogVersion returned error: %v", err)
	}
	if item.PriceVersion != "2027" {
		t.Fatalf("unexpected item: %+v", item)
	}
	input := repo.insertInput
	if input.ProductCode != "yingke" || input.ProductName != "赢客专家" || input.Edition != EditionStandard || input.PriceVersion != "2027" {
		t.Fatalf("input not normalized: %+v", input)
	}
	if input.AnnualPrice.String() != "13200.50" || input.IncrementPrice.String() != "1600" {
		t.Fatalf("amounts not preserved as decimal text: %+v", input)
	}
	if input.Quotas.String() != `{"hour":500,"record":10.5}` {
		t.Fatalf("quotas json = %s", input.Quotas.String())
	}
	if input.Increments.String() != `{"hour":90}` {
		t.Fatalf("increments json = %s", input.Increments.String())
	}
}

func TestCreateCatalogVersionDefaultsEmptyAmountsAndQuantities(t *testing.T) {
	repo := &fakeRepository{}
	service := NewService(repo)

	if _, err := service.CreateCatalogVersion(context.Background(), CreateCatalogRequest{
		ProductCode:  "yingke",
		Edition:      EditionTrial,
		PriceVersion: "2027",
	}); err != nil {
		t.Fatalf("CreateCatalogVersion returned error: %v", err)
	}
	input := repo.insertInput
	if input.AnnualPrice.String() != "0" || input.IncrementPrice.String() != "0" {
		t.Fatalf("empty amounts should fall back to 0: %+v", input)
	}
	if input.Quotas.String() != "{}" || input.Increments.String() != "{}" {
		t.Fatalf("empty quantities should fall back to {}: quotas=%s increments=%s", input.Quotas, input.Increments)
	}
}

func TestCreateCatalogVersionReturnsConflict(t *testing.T) {
	repo := &fakeRepository{insertErr: ErrCatalogVersionExists}
	service := NewService(repo)

	_, err := service.CreateCatalogVersion(context.Background(), CreateCatalogRequest{
		ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "2026",
	})
	if !errors.Is(err, ErrCatalogVersionExists) {
		t.Fatalf("expected ErrCatalogVersionExists, got %v", err)
	}
}

func TestNormalizeLedgerQueryBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		in           LedgerQuery
		wantPage     int
		wantPageSize int
		wantOffset   int64
	}{
		{"defaults", LedgerQuery{TenantID: 1}, 1, 50, 0},
		{"zero values", LedgerQuery{TenantID: 1, Page: 0, PageSize: 0}, 1, 50, 0},
		{"negative values", LedgerQuery{TenantID: 1, Page: -3, PageSize: -8}, 1, 50, 0},
		{"size below default", LedgerQuery{TenantID: 1, Page: 1, PageSize: 1}, 1, 1, 0},
		{"size at default", LedgerQuery{TenantID: 1, Page: 1, PageSize: 50}, 1, 50, 0},
		{"size at max", LedgerQuery{TenantID: 1, Page: 1, PageSize: 200}, 1, 200, 0},
		{"size above max", LedgerQuery{TenantID: 1, Page: 1, PageSize: 201}, 1, 200, 0},
		{"size far above max", LedgerQuery{TenantID: 1, Page: 2, PageSize: 5000}, 2, 200, 200},
		{"second page", LedgerQuery{TenantID: 1, Page: 2, PageSize: 50}, 2, 50, 50},
		{"third page max size", LedgerQuery{TenantID: 1, Page: 3, PageSize: 200}, 3, 200, 400},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := NormalizeLedgerQuery(test.in)
			if got.Page != test.wantPage || got.PageSize != test.wantPageSize {
				t.Fatalf("normalized page/page_size = %d/%d, want %d/%d", got.Page, got.PageSize, test.wantPage, test.wantPageSize)
			}
			if got.Offset() != test.wantOffset {
				t.Fatalf("offset = %d, want %d", got.Offset(), test.wantOffset)
			}
		})
	}

	// 超大 page 不允许把 offset 算成负数（int64 溢出保护）。
	huge := LedgerQuery{TenantID: 1, Page: math.MaxInt, PageSize: MaxLedgerPageSize}
	if offset := huge.Offset(); offset != math.MaxInt64 {
		t.Fatalf("overflow guard offset = %d, want %d", offset, int64(math.MaxInt64))
	}
}

func TestListLedgerValidatesFiltersAndPaginates(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	repo := &fakeRepository{
		ledgerItems: []LedgerEntry{{ID: 1, ProductCode: "yingke", Unit: "hour", Type: LedgerTypeUsage, Amount: Decimal("-1"), CreatedAt: from}},
		ledgerTotal: 1,
	}
	service := NewService(repo)

	page, err := service.ListLedger(context.Background(), LedgerQuery{
		TenantID: 7,
		Product:  " yingke ",
		Unit:     " HOUR ",
		From:     &from,
		To:       &to,
		Page:     -1,
		PageSize: 999,
	})
	if err != nil {
		t.Fatalf("ListLedger returned error: %v", err)
	}
	if page.Total != 1 || page.Page != 1 || page.PageSize != MaxLedgerPageSize {
		t.Fatalf("unexpected page envelope: %+v", page)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected 1 ledger item, got %d", len(page.Items))
	}
	if repo.ledgerQuery.Product != "yingke" || repo.ledgerQuery.Unit != "hour" {
		t.Fatalf("filters not normalized: %+v", repo.ledgerQuery)
	}
	if repo.ledgerQuery.Page != 1 || repo.ledgerQuery.PageSize != MaxLedgerPageSize || repo.ledgerQuery.Offset() != 0 {
		t.Fatalf("pagination not normalized: %+v", repo.ledgerQuery)
	}

	for _, invalid := range []LedgerQuery{
		{TenantID: 7, Unit: "minute"},
		{TenantID: 7, From: &to, To: &from},
		{TenantID: 0},
	} {
		if _, err := service.ListLedger(context.Background(), invalid); err == nil {
			t.Fatalf("expected validation error for %+v", invalid)
		}
	}
}

func TestListLedgerReturnsEmptySliceInsteadOfNil(t *testing.T) {
	service := NewService(&fakeRepository{})
	page, err := service.ListLedger(context.Background(), LedgerQuery{TenantID: 7})
	if err != nil {
		t.Fatalf("ListLedger returned error: %v", err)
	}
	if page.Items == nil {
		t.Fatal("expected empty slice so JSON is [] instead of null")
	}
	if page.Page != DefaultLedgerPage || page.PageSize != DefaultLedgerPageSize {
		t.Fatalf("unexpected default pagination: %+v", page)
	}
}

func TestListEntitlementsDelegatesTenantProductAndIncludeZero(t *testing.T) {
	repo := &fakeRepository{entitlementItems: []Entitlement{{ProductCode: "yingke", Unit: "hour", Balance: Decimal("400")}}}
	service := NewService(repo)

	items, err := service.ListEntitlements(context.Background(), 7, " yingke ", true)
	if err != nil {
		t.Fatalf("ListEntitlements returned error: %v", err)
	}
	if len(items) != 1 || items[0].ProductCode != "yingke" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if repo.entitlementTenantID != 7 || repo.entitlementProduct != "yingke" || !repo.entitlementIncludeZero {
		t.Fatalf("unexpected repository args: tenant=%d product=%q includeZero=%v", repo.entitlementTenantID, repo.entitlementProduct, repo.entitlementIncludeZero)
	}

	if _, err := service.ListEntitlements(context.Background(), 0, "", false); err == nil {
		t.Fatal("expected validation error for tenant_id = 0")
	}
	empty, err := service.ListEntitlements(context.Background(), 8, "", false)
	if err != nil {
		t.Fatalf("ListEntitlements returned error: %v", err)
	}
	if empty == nil {
		t.Fatal("expected empty slice so JSON is [] instead of null")
	}
}

func TestGetServicePeriodReturnsNilWithoutSubscription(t *testing.T) {
	service := NewService(&fakeRepository{})
	period, err := service.GetServicePeriod(context.Background(), 7)
	if err != nil {
		t.Fatalf("GetServicePeriod returned error: %v", err)
	}
	if period != nil {
		t.Fatalf("expected nil service period, got %+v", period)
	}

	started, expired := "2026-11-10", "2027-11-09"
	service = NewService(&fakeRepository{servicePeriod: &ServicePeriod{StartedOn: &started, ExpiredOn: &expired}})
	period, err = service.GetServicePeriod(context.Background(), 7)
	if err != nil || period == nil || *period.StartedOn != started || *period.ExpiredOn != expired {
		t.Fatalf("unexpected service period: %+v err=%v", period, err)
	}
}

func TestDecimalJSONKeepsPrecision(t *testing.T) {
	encoded, err := json.Marshal(CatalogItem{
		ProductCode:    "yingke",
		AnnualPrice:    Decimal("12600.00"),
		Quotas:         Quantities(`{"hour":400}`),
		IncrementPrice: Decimal(""),
		Increments:     Quantities(``),
	})
	if err != nil {
		t.Fatalf("marshal catalog item: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatalf("unmarshal catalog item raw: %v", err)
	}
	// 金额以 JSON 数字输出（不带引号），且原始小数位一个不丢。
	if got := string(raw["annual_price"]); got != "12600.00" {
		t.Fatalf("annual_price = %s, want unquoted 12600.00", got)
	}
	if got := string(raw["increment_price"]); got != "0" {
		t.Fatalf("empty decimal should encode as 0, got %s", got)
	}
	if got := string(raw["quotas"]); got != `{"hour":400}` {
		t.Fatalf("quotas = %s", got)
	}
	if got := string(raw["increments"]); got != "{}" {
		t.Fatalf("empty quantities should encode as {}, got %s", got)
	}
}

func TestDecimalUnmarshalAcceptsNumberAndString(t *testing.T) {
	var payload struct {
		Number Decimal `json:"number"`
		String Decimal `json:"string"`
		Empty  Decimal `json:"empty"`
	}
	if err := json.Unmarshal([]byte(`{"number":12600.50,"string":"123.45","empty":null}`), &payload); err != nil {
		t.Fatalf("unmarshal decimal: %v", err)
	}
	if payload.Number != Decimal("12600.50") || payload.String != Decimal("123.45") || payload.Empty.String() != "0" {
		t.Fatalf("unexpected decimals: %+v", payload)
	}

	var quantities struct {
		Quotas QuantityInputs `json:"quotas"`
	}
	if err := json.Unmarshal([]byte(`{"quotas":{"hour":400,"record":"10.5"}}`), &quantities); err != nil {
		t.Fatalf("unmarshal quantity inputs: %v", err)
	}
	if quantities.Quotas["hour"] != Decimal("400") || quantities.Quotas["record"] != Decimal("10.5") {
		t.Fatalf("unexpected quantity inputs: %+v", quantities.Quotas)
	}
}

func assertValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
}
