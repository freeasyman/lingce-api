package billing

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ---------- fakeRepository 的阶段 C 实现 ----------

func (f *fakeRepository) ensureOrders() {
	if f.orders == nil {
		f.orders = make(map[int64]Order)
	}
}

func (f *fakeRepository) InsertOrder(_ context.Context, input CreateOrderInput) (Order, error) {
	f.insertOrderCalls++
	f.insertOrderInput = input
	f.insertOrderInputs = append(f.insertOrderInputs, input)
	if len(f.insertOrderErrs) > 0 {
		err := f.insertOrderErrs[0]
		f.insertOrderErrs = f.insertOrderErrs[1:]
		if err != nil {
			return Order{}, err
		}
	}
	f.ensureOrders()
	f.nextOrderID++
	now := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
	order := Order{
		ID:               f.nextOrderID,
		OrderNo:          input.OrderNo,
		TenantID:         input.TenantID,
		OrderType:        input.OrderType,
		Items:            input.Items,
		TotalAmount:      input.TotalAmount,
		PayableAmount:    input.PayableAmount,
		Source:           input.Source,
		SalesmanID:       input.SalesmanID,
		ChannelID:        input.ChannelID,
		PriceListVersion: input.PriceListVersion,
		Status:           OrderStatusCreated,
		Reason:           input.Reason,
		ApprovedBy:       input.ApprovedBy,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	f.orders[order.ID] = order
	return order, nil
}

func (f *fakeRepository) ListOrders(_ context.Context, query OrderQuery) ([]Order, int64, error) {
	f.listOrdersQuery = query
	return f.listOrdersItems, f.listOrdersTotal, f.listOrdersErr
}

func (f *fakeRepository) GetOrder(_ context.Context, orderID int64) (Order, error) {
	if f.getOrderErr != nil {
		return Order{}, f.getOrderErr
	}
	order, ok := f.orders[orderID]
	if !ok {
		return Order{}, ErrOrderNotFound
	}
	return order, nil
}

// SettleOrder 模拟 SQL 里的 status='created' 守门。
func (f *fakeRepository) SettleOrder(_ context.Context, input SettleOrderInput) (Order, error) {
	f.settleCalls++
	f.settleInput = input
	if f.settleErr != nil {
		return Order{}, f.settleErr
	}
	order, ok := f.orders[input.OrderID]
	if !ok {
		return Order{}, ErrOrderNotFound
	}
	if order.Status != OrderStatusCreated {
		return Order{}, ErrOrderStatusInvalid
	}
	paidAt := input.PaidAt
	confirmedBy := input.ConfirmedBy
	order.PaidAmount = input.PaidAmount
	order.PaidAt = &paidAt
	order.ConfirmedBy = &confirmedBy
	order.Status = OrderStatusSettled
	f.orders[input.OrderID] = order
	return order, nil
}

// ActivateOrder 模拟事务里的"先抢行再发额度"：
// activated_at 非 NULL 或状态不在允许集合时返回冲突，且不产生任何额度。
func (f *fakeRepository) ActivateOrder(_ context.Context, input ActivateOrderInput) (Order, ServicePeriod, error) {
	f.activateCalls++
	f.activateInput = input
	f.activateInputs = append(f.activateInputs, input)
	if f.activateErr != nil {
		return Order{}, ServicePeriod{}, f.activateErr
	}
	order, ok := f.orders[input.OrderID]
	if !ok {
		return Order{}, ServicePeriod{}, ErrOrderNotFound
	}
	if order.ActivatedAt != nil {
		return Order{}, ServicePeriod{}, ErrOrderAlreadyActivated
	}
	if !containsString(input.AllowedStatuses, order.Status) {
		return Order{}, ServicePeriod{}, ErrOrderStatusInvalid
	}
	activatedAt := time.Date(2026, 11, 10, 10, 30, 0, 0, time.UTC)
	actor := input.ActorID
	order.ActivatedAt = &activatedAt
	order.ActivatedBy = &actor
	f.orders[input.OrderID] = order
	f.grantCalls++
	f.grantedLines = append(f.grantedLines, input.Grants...)

	period := f.activationPeriod
	if period.ExpiredOn == nil {
		started, expired := "2026-11-10", "2027-11-09"
		period = ServicePeriod{StartedOn: &started, ExpiredOn: &expired}
	}
	return order, period, nil
}

// RefundOrder 模拟 SQL 里的 status='settled' AND activated_at IS NULL 守门。
func (f *fakeRepository) RefundOrder(_ context.Context, input RefundOrderInput) (Order, error) {
	f.refundCalls++
	f.refundInput = input
	if f.refundErr != nil {
		return Order{}, f.refundErr
	}
	order, ok := f.orders[input.OrderID]
	if !ok {
		return Order{}, ErrOrderNotFound
	}
	if order.ActivatedAt != nil {
		return Order{}, ErrOrderAlreadyActivated
	}
	if order.Status != OrderStatusSettled {
		return Order{}, ErrOrderStatusInvalid
	}
	order.Status = OrderStatusRefunded
	f.orders[input.OrderID] = order
	return order, nil
}

func (f *fakeRepository) GetAdminDisplayName(_ context.Context, adminID int64) (string, error) {
	f.adminNameID = adminID
	return f.adminName, f.adminNameErr
}

// ---------- 测试辅助 ----------

func newOrderTestService(repo Repository) *Service {
	service := NewService(repo)
	service.now = func() time.Time {
		return time.Date(2026, 11, 10, 10, 30, 45, 0, time.UTC)
	}
	service.orderNoRand = func(int) int { return 123 }
	return service
}

func orderCatalogFixture() []CatalogItem {
	return []CatalogItem{
		{
			ProductCode:    "tingxie",
			ProductName:    "听写病历",
			Edition:        EditionStandard,
			PriceVersion:   "2026",
			AnnualPrice:    Decimal("21600.00"),
			Quotas:         Quantities(`{"record":2500,"round":5000,"hour":400}`),
			IncrementPrice: Decimal("2500.00"),
			Increments:     Quantities(`{"record":500}`),
		},
		{
			ProductCode:    "yingke",
			ProductName:    "赢客专家",
			Edition:        EditionStandard,
			PriceVersion:   "2026",
			AnnualPrice:    Decimal("12600.00"),
			Quotas:         Quantities(`{"hour":400}`),
			IncrementPrice: Decimal("1500.00"),
			Increments:     Quantities(`{"hour":80}`),
		},
		{
			ProductCode:    "tingxie",
			ProductName:    "听写病历",
			Edition:        EditionTrial,
			PriceVersion:   "2026",
			AnnualPrice:    Decimal("0.00"),
			Quotas:         Quantities(`{"record":48,"round":96,"hour":8}`),
			IncrementPrice: Decimal("0.00"),
			Increments:     Quantities(`{}`),
		},
	}
}

func decimalPtr(value string) *Decimal {
	decimal := Decimal(value)
	return &decimal
}

func stringPtr(value string) *string { return &value }

func assertOrderItemsError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an order items error, got nil")
	}
	var items *OrderItemsError
	if !errors.As(err, &items) {
		t.Fatalf("expected *OrderItemsError, got %T: %v", err, err)
	}
}

// ---------- C1 录单 ----------

func TestCreateOrderComputesTotalAndRetriesOrderNo(t *testing.T) {
	repo := &fakeRepository{
		catalogItems:    orderCatalogFixture(),
		insertOrderErrs: []error{ErrOrderNoExists},
	}
	service := newOrderTestService(repo)
	suffixes := []int{111, 222}
	suffixIndex := 0
	service.orderNoRand = func(int) int {
		value := suffixes[suffixIndex]
		suffixIndex++
		return value
	}

	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:         9,
		OrderType:        OrderTypeSingle,
		PriceListVersion: " 2026 ",
		Items: []CreateOrderItem{
			{
				ProductCode: "tingxie",
				Edition:     "standard",
				QuotaType:   "official",
				Quotas:      QuantityInputs{"record": Decimal("2500"), "round": Decimal("5000"), "hour": Decimal("400")},
			},
			{
				ProductCode: " yingke ",
				Edition:     " Standard ",
				Quotas:      QuantityInputs{"hour": Decimal("400")},
				Price:       decimalPtr("12600"),
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if repo.insertOrderCalls != 2 {
		t.Fatalf("order_no conflict should retry exactly once, insert calls = %d", repo.insertOrderCalls)
	}
	if len(repo.insertOrderInputs) != 2 {
		t.Fatalf("expected 2 insert attempts, got %d", len(repo.insertOrderInputs))
	}
	first, second := repo.insertOrderInputs[0], repo.insertOrderInputs[1]
	if first.OrderNo != "BL20261110103045111" || second.OrderNo != "BL20261110103045222" {
		t.Fatalf("unexpected order numbers: %q / %q", first.OrderNo, second.OrderNo)
	}
	if second.TotalAmount != Decimal("34200.00") || second.PayableAmount != Decimal("34200.00") {
		t.Fatalf("unexpected amounts: total=%s payable=%s", second.TotalAmount, second.PayableAmount)
	}
	if second.PriceListVersion != "2026" {
		t.Fatalf("price_list_version = %q, want 2026", second.PriceListVersion)
	}
	if second.Source != OrderSourceDirect {
		t.Fatalf("source should default to direct, got %q", second.Source)
	}
	if len(second.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(second.Items))
	}
	if second.Items[0].Price != Decimal("21600.00") {
		t.Fatalf("tingxie price should come from catalog annual_price, got %s", second.Items[0].Price)
	}
	if second.Items[1].Price != Decimal("12600.00") || second.Items[1].ProductCode != "yingke" || second.Items[1].Edition != EditionStandard {
		t.Fatalf("unexpected second item: %+v", second.Items[1])
	}
	if order.OrderNo != second.OrderNo || order.Status != OrderStatusCreated || order.TotalAmount != Decimal("34200.00") {
		t.Fatalf("unexpected created order: %+v", order)
	}
}

func TestCreateOrderForcesGiftAmountsToZero(t *testing.T) {
	repo := &fakeRepository{catalogItems: orderCatalogFixture()}
	service := newOrderTestService(repo)

	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: " gift ",
		Reason:    " 老板同意补 500 份 ",
		Items: []CreateOrderItem{{
			ProductCode: "tingxie",
			Edition:     "standard",
			Quotas:      QuantityInputs{"record": Decimal("500")},
		}},
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if order.TotalAmount != Decimal("0.00") || order.PayableAmount != Decimal("0.00") {
		t.Fatalf("gift order amounts must be forced to 0, got total=%s payable=%s", order.TotalAmount, order.PayableAmount)
	}
	if len(order.Items) != 1 || order.Items[0].Price != Decimal("21600.00") {
		t.Fatalf("gift item should keep its equivalent price, got %+v", order.Items)
	}
	if order.Reason != "老板同意补 500 份" {
		t.Fatalf("reason not trimmed: %q", order.Reason)
	}
	if repo.insertOrderCalls != 1 {
		t.Fatalf("expected 1 insert, got %d", repo.insertOrderCalls)
	}

	_, err = service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeGift,
		Items: []CreateOrderItem{{
			ProductCode: "tingxie",
			Edition:     "standard",
			Quotas:      QuantityInputs{"record": Decimal("500")},
		}},
	})
	assertValidationError(t, err)
	if repo.insertOrderCalls != 1 {
		t.Fatalf("gift without reason must not be inserted, insert calls = %d", repo.insertOrderCalls)
	}
}

func TestCreateOrderIncrementUsesIncrementPrice(t *testing.T) {
	repo := &fakeRepository{catalogItems: orderCatalogFixture()}
	service := newOrderTestService(repo)

	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeIncrement,
		Items: []CreateOrderItem{{
			ProductCode: "tingxie",
			Edition:     "standard",
			Quotas:      QuantityInputs{"record": Decimal("500")},
		}},
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if order.TotalAmount != Decimal("2500.00") || order.PayableAmount != Decimal("2500.00") {
		t.Fatalf("increment order should use increment_price, got total=%s", order.TotalAmount)
	}
	if order.PriceListVersion != "2026" {
		t.Fatalf("price_list_version should be backfilled from catalog, got %q", order.PriceListVersion)
	}
}

func TestCreateOrderRejectsInvalidItems(t *testing.T) {
	base := func() CreateOrderRequest {
		return CreateOrderRequest{
			TenantID:  9,
			OrderType: OrderTypeSingle,
			Items: []CreateOrderItem{{
				ProductCode: "tingxie",
				Edition:     "standard",
				Quotas:      QuantityInputs{"record": Decimal("500")},
			}},
		}
	}
	cases := []struct {
		name   string
		mutate func(*CreateOrderRequest)
	}{
		{"empty items", func(req *CreateOrderRequest) { req.Items = nil }},
		{"unsupported unit", func(req *CreateOrderRequest) {
			req.Items[0].Quotas = QuantityInputs{"minute": Decimal("10")}
		}},
		{"zero quota", func(req *CreateOrderRequest) {
			req.Items[0].Quotas = QuantityInputs{"record": Decimal("0")}
		}},
		{"negative quota", func(req *CreateOrderRequest) {
			req.Items[0].Quotas = QuantityInputs{"record": Decimal("-1")}
		}},
		{"empty quotas", func(req *CreateOrderRequest) { req.Items[0].Quotas = nil }},
		{"invalid edition", func(req *CreateOrderRequest) { req.Items[0].Edition = "enterprise" }},
		{"invalid quota type", func(req *CreateOrderRequest) { req.Items[0].QuotaType = "package" }},
		{"product missing from catalog and price missing", func(req *CreateOrderRequest) {
			req.Items[0].ProductCode = "unknown"
		}},
		{"negative price", func(req *CreateOrderRequest) { req.Items[0].Price = decimalPtr("-1") }},
		{"too many decimal places", func(req *CreateOrderRequest) { req.Items[0].Price = decimalPtr("21600.005") }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepository{catalogItems: orderCatalogFixture()}
			service := newOrderTestService(repo)
			req := base()
			test.mutate(&req)
			_, err := service.CreateOrder(context.Background(), req)
			assertOrderItemsError(t, err)
			if repo.insertOrderCalls != 0 {
				t.Fatalf("invalid items must not be inserted, insert calls = %d", repo.insertOrderCalls)
			}
		})
	}
}

func TestCreateOrderRejectsInvalidEnums(t *testing.T) {
	item := []CreateOrderItem{{
		ProductCode: "tingxie",
		Edition:     "standard",
		Quotas:      QuantityInputs{"record": Decimal("500")},
	}}
	cases := []struct {
		name string
		req  CreateOrderRequest
	}{
		{"missing tenant", CreateOrderRequest{OrderType: OrderTypeSingle, Items: item}},
		{"invalid order type", CreateOrderRequest{TenantID: 9, OrderType: "renew", Items: item}},
		{"invalid source", CreateOrderRequest{TenantID: 9, OrderType: OrderTypeSingle, Source: "agent", Items: item}},
		{"zero salesman", func() CreateOrderRequest {
			zero := int64(0)
			return CreateOrderRequest{TenantID: 9, OrderType: OrderTypeSingle, Items: item, SalesmanID: &zero}
		}()},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepository{catalogItems: orderCatalogFixture()}
			service := newOrderTestService(repo)
			_, err := service.CreateOrder(context.Background(), test.req)
			assertValidationError(t, err)
			if repo.insertOrderCalls != 0 {
				t.Fatalf("invalid order must not be inserted, insert calls = %d", repo.insertOrderCalls)
			}
		})
	}
}

func TestCreateOrderTrialForcesTrialQuotaType(t *testing.T) {
	repo := &fakeRepository{catalogItems: orderCatalogFixture()}
	service := newOrderTestService(repo)

	order, err := service.CreateOrder(context.Background(), CreateOrderRequest{
		TenantID:  9,
		OrderType: OrderTypeTrial,
		Items: []CreateOrderItem{{
			ProductCode: "tingxie",
			Edition:     "trial",
			QuotaType:   "official",
			Quotas:      QuantityInputs{"record": Decimal("48")},
		}},
	})
	if err != nil {
		t.Fatalf("CreateOrder returned error: %v", err)
	}
	if order.Items[0].QuotaType != QuotaTypeTrial {
		t.Fatalf("trial order must grant trial quota, got %q", order.Items[0].QuotaType)
	}
}

// ---------- C2 回款确认 ----------

func TestSettleOrderRequiresFullPayment(t *testing.T) {
	repo := &fakeRepository{orders: map[int64]Order{
		1: {
			ID:            1,
			OrderNo:       "BL20261110103045123",
			TenantID:      9,
			OrderType:     OrderTypeSingle,
			PayableAmount: Decimal("21600.00"),
			Status:        OrderStatusCreated,
		},
	}}
	service := newOrderTestService(repo)
	paidAt := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

	if _, err := service.SettleOrder(context.Background(), 1, SettleOrderRequest{PaidAmount: decimalPtr("20000")}, 42); !errors.Is(err, ErrOrderAmountMismatch) {
		t.Fatalf("partial payment should be ORDER_AMOUNT_MISMATCH, got %v", err)
	}
	if repo.settleCalls != 0 {
		t.Fatalf("amount mismatch must not call store, calls=%d", repo.settleCalls)
	}
	if _, err := service.SettleOrder(context.Background(), 1, SettleOrderRequest{}, 42); err == nil {
		t.Fatal("missing paid_amount should be rejected")
	}
	if _, err := service.SettleOrder(context.Background(), 99, SettleOrderRequest{PaidAmount: decimalPtr("0")}, 42); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("missing order should be ORDER_NOT_FOUND, got %v", err)
	}

	order, err := service.SettleOrder(context.Background(), 1, SettleOrderRequest{
		PaidAmount: decimalPtr("21600"),
		PaidAt:     &paidAt,
	}, 42)
	if err != nil {
		t.Fatalf("SettleOrder returned error: %v", err)
	}
	if repo.settleInput.ConfirmedBy != 42 || repo.settleInput.PaidAmount != Decimal("21600.00") {
		t.Fatalf("unexpected settle input: %+v", repo.settleInput)
	}
	if !repo.settleInput.PaidAt.Equal(paidAt) {
		t.Fatalf("paid_at not passed through: %v", repo.settleInput.PaidAt)
	}
	if order.Status != OrderStatusSettled || order.ConfirmedBy == nil || *order.ConfirmedBy != 42 {
		t.Fatalf("unexpected settled order: %+v", order)
	}
	if order.PaidAt == nil || !order.PaidAt.Equal(paidAt) {
		t.Fatalf("unexpected paid_at: %v", order.PaidAt)
	}

	// 已确认的订单不能再确认一次。
	if _, err := service.SettleOrder(context.Background(), 1, SettleOrderRequest{PaidAmount: decimalPtr("21600")}, 42); !errors.Is(err, ErrOrderStatusInvalid) {
		t.Fatalf("settling a settled order should be ORDER_STATUS_INVALID, got %v", err)
	}
	if repo.settleCalls != 1 {
		t.Fatalf("store should be called once, calls=%d", repo.settleCalls)
	}
}

// ---------- C3 开通 ----------

func TestActivateOrderGrantsOnceAndReturnsEntitlements(t *testing.T) {
	repo := &fakeRepository{
		orders: map[int64]Order{
			1: {
				ID:            1,
				OrderNo:       "BL20261110103045123",
				TenantID:      9,
				OrderType:     OrderTypeSingle,
				PayableAmount: Decimal("21600.00"),
				Status:        OrderStatusSettled,
				Items: []OrderItem{{
					ProductCode: "tingxie",
					Edition:     EditionStandard,
					QuotaType:   QuotaTypeOfficial,
					Quotas:      Quantities(`{"record":2500,"round":5000,"hour":400}`),
					Price:       Decimal("21600.00"),
				}},
			},
		},
		entitlementItems: []Entitlement{{
			ProductCode: "tingxie",
			Unit:        "record",
			Edition:     EditionStandard,
			QuotaType:   QuotaTypeOfficial,
			Granted:     Decimal("2500"),
			Balance:     Decimal("2500"),
		}},
		adminName: "财务小李",
		activationPeriod: ServicePeriod{
			StartedOn: stringPtr("2026-11-10"),
			ExpiredOn: stringPtr("2027-11-09"),
		},
	}
	service := newOrderTestService(repo)

	result, err := service.ActivateOrder(context.Background(), 1, 42)
	if err != nil {
		t.Fatalf("ActivateOrder returned error: %v", err)
	}
	if result.Order.ActivatedAt == nil || result.Order.ActivatedBy == nil || *result.Order.ActivatedBy != 42 {
		t.Fatalf("order not marked activated: %+v", result.Order)
	}
	if result.ServicePeriod == nil || *result.ServicePeriod.ExpiredOn != "2027-11-09" {
		t.Fatalf("unexpected service period: %+v", result.ServicePeriod)
	}
	if len(result.Entitlements) != 1 || result.Entitlements[0].Balance != Decimal("2500") {
		t.Fatalf("unexpected entitlements: %+v", result.Entitlements)
	}
	if repo.grantCalls != 1 || repo.entitlementCalls != 1 {
		t.Fatalf("grant calls=%d entitlement calls=%d", repo.grantCalls, repo.entitlementCalls)
	}
	if repo.activateInput.Operator != "财务小李" || repo.adminNameID != 42 {
		t.Fatalf("operator not resolved: %+v (adminNameID=%d)", repo.activateInput.Operator, repo.adminNameID)
	}
	if repo.activateInput.LedgerSource != LedgerSourcePurchase {
		t.Fatalf("single order ledger source = %q, want purchase", repo.activateInput.LedgerSource)
	}
	if repo.activateInput.Reason != "回款确认按单下发（订单 BL20261110103045123）" {
		t.Fatalf("unexpected ledger reason: %q", repo.activateInput.Reason)
	}
	if len(repo.activateInput.AllowedStatuses) != 1 || repo.activateInput.AllowedStatuses[0] != OrderStatusSettled {
		t.Fatalf("paid order should only activate from settled, got %v", repo.activateInput.AllowedStatuses)
	}
	wantUnits := []string{"hour", "record", "round"}
	if len(repo.grantedLines) != len(wantUnits) {
		t.Fatalf("expected %d grant lines, got %d: %+v", len(wantUnits), len(repo.grantedLines), repo.grantedLines)
	}
	for index, unit := range wantUnits {
		if repo.grantedLines[index].Unit != unit {
			t.Fatalf("grant line %d unit = %q, want %q", index, repo.grantedLines[index].Unit, unit)
		}
	}

	// 幂等：第二次开通必须被拒，且不能再发额度。
	if _, err := service.ActivateOrder(context.Background(), 1, 42); !errors.Is(err, ErrOrderAlreadyActivated) {
		t.Fatalf("second activation should be ORDER_ALREADY_ACTIVATED, got %v", err)
	}
	if repo.grantCalls != 1 {
		t.Fatalf("activation must not grant twice, grant calls = %d", repo.grantCalls)
	}
	if repo.entitlementCalls != 1 {
		t.Fatalf("rejected activation should not read entitlements again, calls = %d", repo.entitlementCalls)
	}
}

func TestActivateOrderPropagatesRowClaimConflict(t *testing.T) {
	// 模拟并发开通：service 预检时 activated_at 仍为 NULL，但事务内 UPDATE 影响 0 行。
	repo := &fakeRepository{
		orders: map[int64]Order{
			1: {
				ID:        1,
				OrderNo:   "BL1",
				TenantID:  9,
				OrderType: OrderTypeSingle,
				Status:    OrderStatusSettled,
				Items: []OrderItem{{
					ProductCode: "tingxie",
					Edition:     EditionStandard,
					Quotas:      Quantities(`{"record":1}`),
				}},
			},
		},
		activateErr: ErrOrderAlreadyActivated,
	}
	service := newOrderTestService(repo)

	if _, err := service.ActivateOrder(context.Background(), 1, 42); !errors.Is(err, ErrOrderAlreadyActivated) {
		t.Fatalf("row claim conflict should be ORDER_ALREADY_ACTIVATED, got %v", err)
	}
	if repo.entitlementCalls != 0 {
		t.Fatalf("conflict must not read entitlements, calls = %d", repo.entitlementCalls)
	}
}

func TestActivateOrderAllowsGiftAndTrialFromCreated(t *testing.T) {
	item := func() []OrderItem {
		return []OrderItem{{
			ProductCode: "tingxie",
			Edition:     EditionStandard,
			QuotaType:   QuotaTypeOfficial,
			Quotas:      Quantities(`{"record":500}`),
		}}
	}

	// 正式订单必须回款后才能开通。
	repo := &fakeRepository{orders: map[int64]Order{
		1: {ID: 1, OrderNo: "BL1", TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusCreated, Items: item()},
	}}
	service := newOrderTestService(repo)
	if _, err := service.ActivateOrder(context.Background(), 1, 42); !errors.Is(err, ErrOrderStatusInvalid) {
		t.Fatalf("unsettled paid order should be ORDER_STATUS_INVALID, got %v", err)
	}
	if repo.activateCalls != 0 {
		t.Fatalf("unsettled order must not reach store, calls=%d", repo.activateCalls)
	}

	// 赠送单可以从 created 直接开通，source=gift。
	repo = &fakeRepository{orders: map[int64]Order{
		2: {
			ID: 2, OrderNo: "BLGIFT", TenantID: 9, OrderType: OrderTypeGift,
			Status: OrderStatusCreated, Reason: "老板同意", Items: item(),
		},
	}}
	service = newOrderTestService(repo)
	if _, err := service.ActivateOrder(context.Background(), 2, 42); err != nil {
		t.Fatalf("gift activation returned error: %v", err)
	}
	if repo.activateInput.LedgerSource != LedgerSourceGift {
		t.Fatalf("gift ledger source = %q", repo.activateInput.LedgerSource)
	}
	if repo.activateInput.Reason != "赠送：老板同意（订单 BLGIFT）" {
		t.Fatalf("unexpected gift ledger reason: %q", repo.activateInput.Reason)
	}
	if !containsString(repo.activateInput.AllowedStatuses, OrderStatusCreated) {
		t.Fatalf("gift order should allow created status, got %v", repo.activateInput.AllowedStatuses)
	}

	// 试用单可以从 created 直接开通，额度必须是 trial。
	repo = &fakeRepository{orders: map[int64]Order{
		3: {
			ID: 3, OrderNo: "BLTRIAL", TenantID: 9, OrderType: OrderTypeTrial,
			Status: OrderStatusCreated,
			Items: []OrderItem{{
				ProductCode: "tingxie", Edition: EditionTrial, QuotaType: QuotaTypeOfficial,
				Quotas: Quantities(`{"record":48}`),
			}},
		},
	}}
	service = newOrderTestService(repo)
	if _, err := service.ActivateOrder(context.Background(), 3, 42); err != nil {
		t.Fatalf("trial activation returned error: %v", err)
	}
	if repo.activateInput.LedgerSource != LedgerSourceTrial {
		t.Fatalf("trial ledger source = %q", repo.activateInput.LedgerSource)
	}
	if len(repo.grantedLines) != 1 || repo.grantedLines[0].QuotaType != QuotaTypeTrial {
		t.Fatalf("trial order must grant trial quota, got %+v", repo.grantedLines)
	}
}

// ---------- C4 退款 ----------

func TestRefundOrderRejectedAfterActivation(t *testing.T) {
	activatedAt := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	repo := &fakeRepository{orders: map[int64]Order{
		1: {ID: 1, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled, ActivatedAt: &activatedAt},
		2: {ID: 2, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled, PayableAmount: Decimal("21600.00")},
		3: {ID: 3, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusCreated},
	}}
	service := newOrderTestService(repo)

	if _, err := service.RefundOrder(context.Background(), 1, 42); !errors.Is(err, ErrOrderAlreadyActivated) {
		t.Fatalf("refund after activation should be ORDER_ALREADY_ACTIVATED, got %v", err)
	}
	if repo.refundCalls != 0 {
		t.Fatalf("activated order must not reach store, calls=%d", repo.refundCalls)
	}
	if _, err := service.RefundOrder(context.Background(), 3, 42); !errors.Is(err, ErrOrderStatusInvalid) {
		t.Fatalf("refunding an unsettled order should be ORDER_STATUS_INVALID, got %v", err)
	}
	if _, err := service.RefundOrder(context.Background(), 99, 42); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("missing order should be ORDER_NOT_FOUND, got %v", err)
	}

	refunded, err := service.RefundOrder(context.Background(), 2, 42)
	if err != nil {
		t.Fatalf("RefundOrder returned error: %v", err)
	}
	if refunded.Status != OrderStatusRefunded || repo.refundCalls != 1 {
		t.Fatalf("unexpected refunded order: %+v calls=%d", refunded, repo.refundCalls)
	}

	// 并发场景：预检通过但事务内条件已不满足（被激活），必须返回冲突而不是当成成功。
	repo.orders[2] = Order{ID: 2, TenantID: 9, OrderType: OrderTypeSingle, Status: OrderStatusSettled}
	repo.refundErr = ErrOrderAlreadyActivated
	if _, err := service.RefundOrder(context.Background(), 2, 42); !errors.Is(err, ErrOrderAlreadyActivated) {
		t.Fatalf("store conflict should be ORDER_ALREADY_ACTIVATED, got %v", err)
	}
}

// ---------- 列表 / 详情 ----------

func TestListOrdersNormalizesFiltersAndPaginates(t *testing.T) {
	repo := &fakeRepository{
		listOrdersItems: []Order{{ID: 1, OrderNo: "BL1"}},
		listOrdersTotal: 1,
	}
	service := NewService(repo)

	page, err := service.ListOrders(context.Background(), OrderQuery{
		TenantID:  9,
		Status:    " SETTLED ",
		OrderType: " SINGLE ",
		Page:      -5,
		PageSize:  999,
	})
	if err != nil {
		t.Fatalf("ListOrders returned error: %v", err)
	}
	if page.Page != DefaultOrderPage || page.PageSize != MaxOrderPageSize || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("unexpected page envelope: %+v", page)
	}
	if repo.listOrdersQuery.Status != OrderStatusSettled || repo.listOrdersQuery.OrderType != OrderTypeSingle {
		t.Fatalf("filters not normalized: %+v", repo.listOrdersQuery)
	}
	if repo.listOrdersQuery.TenantID != 9 || repo.listOrdersQuery.Offset() != 0 {
		t.Fatalf("unexpected query: %+v", repo.listOrdersQuery)
	}

	emptyRepo := &fakeRepository{}
	emptyPage, err := NewService(emptyRepo).ListOrders(context.Background(), OrderQuery{})
	if err != nil {
		t.Fatalf("ListOrders returned error: %v", err)
	}
	if emptyPage.Items == nil {
		t.Fatal("expected empty slice so JSON is [] instead of null")
	}

	for _, invalid := range []OrderQuery{
		{TenantID: -1},
		{Status: "unknown"},
		{OrderType: "renew"},
	} {
		if _, err := service.ListOrders(context.Background(), invalid); err == nil {
			t.Fatalf("expected validation error for %+v", invalid)
		}
	}
}

func TestGetOrderReturnsNotFound(t *testing.T) {
	service := NewService(&fakeRepository{})
	if _, err := service.GetOrder(context.Background(), 1); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("expected ORDER_NOT_FOUND, got %v", err)
	}
	if _, err := service.GetOrder(context.Background(), 0); err == nil {
		t.Fatal("expected validation error for order id 0")
	}
}

// ---------- 金额与发额度明细 ----------

func TestParseAmountCents(t *testing.T) {
	valid := []struct {
		input Decimal
		cents int64
		text  string
	}{
		{"21600", 2160000, "21600.00"},
		{"21600.5", 2160050, "21600.50"},
		{"21600.05", 2160005, "21600.05"},
		{"0.01", 1, "0.01"},
		{"", 0, "0.00"},
		{"9999999999.99", 999999999999, "9999999999.99"},
	}
	for _, test := range valid {
		cents, err := parseAmountCents(test.input)
		if err != nil {
			t.Fatalf("parse %q: %v", test.input, err)
		}
		if cents != test.cents {
			t.Fatalf("parse %q = %d cents, want %d", test.input, cents, test.cents)
		}
		if got := centsToDecimal(cents); got != Decimal(test.text) {
			t.Fatalf("format %d = %s, want %s", cents, got, test.text)
		}
	}

	invalid := []Decimal{"-1", "abc", "1e3", "1.005", "10000000000", ".", "1.2.3"}
	for _, input := range invalid {
		if _, err := parseAmountCents(input); err == nil {
			t.Fatalf("parse %q should fail", input)
		}
	}

	// 分类加总，避免 0.1 + 0.2 的浮点误差。
	first, _ := parseAmountCents(Decimal("0.1"))
	second, _ := parseAmountCents(Decimal("0.2"))
	if got := centsToDecimal(first + second); got != Decimal("0.30") {
		t.Fatalf("0.1 + 0.2 = %s, want 0.30", got)
	}
}

func TestOrderGrantLinesSortsAndValidates(t *testing.T) {
	order := Order{
		OrderType: OrderTypeSingle,
		Items: []OrderItem{{
			ProductCode: "tingxie",
			Edition:     "standard",
			QuotaType:   "official",
			Quotas:      Quantities(`{"ROUND":5000,"record":2500,"hour":400}`),
		}},
	}
	lines, err := order.GrantLines()
	if err != nil {
		t.Fatalf("GrantLines returned error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 grant lines, got %d", len(lines))
	}
	want := []string{"hour", "record", "round"}
	for index, unit := range want {
		if lines[index].Unit != unit {
			t.Fatalf("grant line %d unit = %q, want %q", index, lines[index].Unit, unit)
		}
	}
	if lines[1].Amount != Decimal("2500") || lines[1].ProductCode != "tingxie" || lines[1].Edition != EditionStandard {
		t.Fatalf("unexpected grant line: %+v", lines[1])
	}

	badOrders := []Order{
		{Items: nil},
		{Items: []OrderItem{{ProductCode: "tingxie", Quotas: Quantities(`not-json`)}}},
		{Items: []OrderItem{{ProductCode: "tingxie", Quotas: Quantities(`{}`)}}},
		{Items: []OrderItem{{ProductCode: "tingxie", Quotas: Quantities(`{"minute":10}`)}}},
		{Items: []OrderItem{{ProductCode: "tingxie", Quotas: Quantities(`{"record":0}`)}}},
		{Items: []OrderItem{{ProductCode: "tingxie", Edition: "enterprise", Quotas: Quantities(`{"record":1}`)}}},
	}
	for index, bad := range badOrders {
		if _, err := bad.GrantLines(); err == nil {
			t.Fatalf("bad order %d should fail GrantLines", index)
		} else {
			assertOrderItemsError(t, err)
		}
	}
}
