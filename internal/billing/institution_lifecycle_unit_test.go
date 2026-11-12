package billing

// 第 2 批（暂停 / 取消 / 作废订单 / 调额度）的纯逻辑单测：不依赖数据库。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeLifecycleRequests(t *testing.T) {
	if _, err := (SuspendRequest{}).normalize(); err == nil {
		t.Fatal("暂停原因为空应报错")
	}
	if _, err := (SuspendRequest{Reason: "  机房租约纠纷  "}).normalize(); err != nil {
		t.Fatalf("合法暂停原因被拒：%v", err)
	}

	if _, err := (CancelRequest{Reason: "客户要求终止"}).normalize(); err == nil {
		t.Fatal("未勾选确认的取消应报错")
	}
	if _, err := (CancelRequest{Reason: "客户要求终止", Confirmed: true}).normalize(); err != nil {
		t.Fatalf("合法取消请求被拒：%v", err)
	}

	if _, err := (VoidOrderRequest{}).normalize(); err == nil {
		t.Fatal("作废原因为空应报错")
	}
	longReason := strings.Repeat("原", maxLifecycleReasonLength+1)
	if _, err := (VoidOrderRequest{Reason: longReason}).normalize(); err == nil {
		t.Fatal("作废原因超长应报错")
	}
}

func TestNormalizeEntitlementCorrectionRequest(t *testing.T) {
	base := EntitlementCorrectionRequest{
		ProductCode: "  YingKe ",
		Unit:        " HOUR ",
		Amount:      "-12.5",
		Reason:      "  补录差异  ",
	}
	got, err := base.normalize()
	if err != nil {
		t.Fatalf("合法调额度请求被拒：%v", err)
	}
	if got.ProductCode != "yingke" || got.Unit != "hour" || got.Amount != Decimal("-12.50") ||
		got.Reason != "补录差异" {
		t.Fatalf("归一化结果不对：%+v", got)
	}

	cases := map[string]EntitlementCorrectionRequest{
		"缺产品":     {Unit: "hour", Amount: "1", Reason: "x"},
		"缺单位":     {ProductCode: "yingke", Amount: "1", Reason: "x"},
		"单位非法":    {ProductCode: "yingke", Unit: "day", Amount: "1", Reason: "x"},
		"金额为0":    {ProductCode: "yingke", Unit: "hour", Amount: "0", Reason: "x"},
		"缺原因":     {ProductCode: "yingke", Unit: "hour", Amount: "1"},
		"snake别名": {SnakeProductCode: "yingke", Unit: "hour", Amount: "0", Reason: "x"},
	}
	for name, req := range cases {
		if _, err := req.normalize(); err == nil {
			t.Fatalf("%s 应报错", name)
		}
	}
}

func TestCorrectionNeedsApproval(t *testing.T) {
	product := catalogProduct{
		Code: "yingke",
		Increments: []quotaPair{
			{Unit: "hour", Amount: "80"},
		},
	}
	cases := []struct {
		name     string
		unit     string
		amount   int64
		wantPack int64
		wantNeed bool
	}{
		{"below one pack", "hour", 7900, 8000, false},
		{"exactly one pack", "hour", 8000, 8000, true},
		{"negative one pack", "hour", -8000, 8000, true},
		{"no increment spec falls back to one unit", "record", 100, 100, true},
		{"no increment spec below one unit", "record", 99, 100, false},
	}
	for _, item := range cases {
		pack, need := correctionNeedsApproval(product, item.unit, item.amount)
		if pack != item.wantPack || need != item.wantNeed {
			t.Fatalf("%s: pack=%d need=%v, want %d/%v", item.name, pack, need, item.wantPack, item.wantNeed)
		}
	}
}

func TestInstitutionStatusPriority(t *testing.T) {
	today := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	tomorrow := today.AddDate(0, 0, 1)
	pausedAt := today.Add(-time.Hour)
	cancelledAt := today.Add(-time.Minute)
	periodID := int64(7)
	active := PeriodStatusActive
	expired := PeriodStatusExpired
	terminated := PeriodStatusTerminated
	endFuture := formatDateOnly(tomorrow)
	endPast := formatDateOnly(yesterday)

	cases := []struct {
		name string
		base institutionBaseRow
		mode string
		want string
	}{
		{
			name: "cancelled wins over everything",
			base: institutionBaseRow{CancelledAt: &cancelledAt, PausedAt: &pausedAt, PeriodID: &periodID,
				PeriodStatus: active, ExpiredOn: &endFuture},
			mode: AccountModeFormal, want: InstitutionStatusCancelled,
		},
		{
			name: "paused wins over expired",
			base: institutionBaseRow{PausedAt: &pausedAt, PeriodID: &periodID,
				PeriodStatus: active, ExpiredOn: &endPast},
			mode: AccountModeFormal, want: InstitutionStatusPaused,
		},
		{
			name: "no period is unopened",
			base: institutionBaseRow{},
			mode: AccountModeFormal, want: InstitutionStatusUnopened,
		},
		{
			name: "terminated period is unopened",
			base: institutionBaseRow{PeriodID: &periodID, PeriodStatus: terminated, ExpiredOn: &endPast},
			mode: AccountModeFormal, want: InstitutionStatusUnopened,
		},
		{
			name: "period status expired",
			base: institutionBaseRow{PeriodID: &periodID, PeriodStatus: expired, ExpiredOn: &endFuture},
			mode: AccountModeFormal, want: InstitutionStatusExpired,
		},
		{
			name: "active but end date passed",
			base: institutionBaseRow{PeriodID: &periodID, PeriodStatus: active, ExpiredOn: &endPast},
			mode: AccountModeFormal, want: InstitutionStatusExpired,
		},
		{
			name: "trial not expired",
			base: institutionBaseRow{PeriodID: &periodID, PeriodStatus: active, ExpiredOn: &endFuture},
			mode: AccountModeTrial, want: InstitutionStatusTrial,
		},
		{
			name: "formal active",
			base: institutionBaseRow{PeriodID: &periodID, PeriodStatus: active, ExpiredOn: &endFuture},
			mode: AccountModeFormal, want: InstitutionStatusActive,
		},
	}
	for _, item := range cases {
		if got := institutionStatus(item.base, item.mode, today); got != item.want {
			t.Fatalf("%s: status=%s, want %s", item.name, got, item.want)
		}
	}
}

func TestVoidableOrderGuard(t *testing.T) {
	now := time.Now()
	paid := Order{Status: OrderStatusPaid, PaidAmount: "100.00"}
	if err := voidableOrderGuard(paid); !errors.Is(err, ErrOrderNotVoidable) {
		t.Fatalf("已收款订单应拒绝作废，实际 %v", err)
	}
	activated := Order{Status: OrderStatusCreated, PaidAmount: "0", ActivatedAt: &now}
	if err := voidableOrderGuard(activated); !errors.Is(err, ErrOrderNotVoidable) {
		t.Fatalf("已核销订单应拒绝作废，实际 %v", err)
	}
	if err := voidableOrderGuard(Order{Status: OrderStatusCreated, PaidAmount: "0"}); err != nil {
		t.Fatalf("未收款未核销订单应允许作废，实际 %v", err)
	}
	if err := voidableOrderGuard(Order{Status: OrderStatusVoid, PaidAmount: "0"}); err != nil {
		t.Fatalf("已作废订单在幂等分支前不应报错，实际 %v", err)
	}
}

func TestDeductRejectedWhenPausedOrCancelled(t *testing.T) {
	t.Run("paused", func(t *testing.T) {
		service, repo := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))
		repo.subscriptionState = InstitutionStatusPaused
		_, err := service.Deduct(context.Background(), deductInput(9, "emr:record:1:draft",
			DeductItem{Unit: "hour", Amount: 1}))
		if !errors.Is(err, ErrSubscriptionPaused) {
			t.Fatalf("暂停机构扣量应返回 ErrSubscriptionPaused，实际 %v", err)
		}
		if repo.beginDeductCalls != 0 || len(repo.usageLedger) != 0 {
			t.Fatalf("被闸门拒绝的扣量不应开启事务/写流水：begin=%d ledger=%d",
				repo.beginDeductCalls, len(repo.usageLedger))
		}
		if err := service.CheckQuota(context.Background(), 9, "yingke", []DeductItem{{Unit: "hour", Amount: 1}}); !errors.Is(err, ErrSubscriptionPaused) {
			t.Fatalf("暂停机构预检应返回 ErrSubscriptionPaused，实际 %v", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		service, repo := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))
		repo.subscriptionState = InstitutionStatusCancelled
		_, err := service.Deduct(context.Background(), deductInput(9, "emr:record:2:draft",
			DeductItem{Unit: "hour", Amount: 1}))
		if !errors.Is(err, ErrSubscriptionCancelled) {
			t.Fatalf("取消机构扣量应返回 ErrSubscriptionCancelled，实际 %v", err)
		}
		if err := service.CheckQuota(context.Background(), 9, "yingke", []DeductItem{{Unit: "hour", Amount: 1}}); !errors.Is(err, ErrSubscriptionCancelled) {
			t.Fatalf("取消机构预检应返回 ErrSubscriptionCancelled，实际 %v", err)
		}
	})

	t.Run("normal", func(t *testing.T) {
		service, _ := newDeductTestService(9, officialRow("hour", "400", "0", "0", "400"))
		if _, err := service.Deduct(context.Background(), deductInput(9, "emr:record:3:draft",
			DeductItem{Unit: "hour", Amount: 1})); err != nil {
			t.Fatalf("正常机构扣量应成功：%v", err)
		}
	})
}

func TestLifecycleHTTPErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"paused", ErrSubscriptionPaused, http.StatusConflict, "SUBSCRIPTION_PAUSED"},
		{"cancelled sentinel", ErrSubscriptionCancelled, http.StatusConflict, "SUBSCRIPTION_CANCELLED"},
		{"cancelled conflict", &ConflictError{Sentinel: ErrInstitutionCancelled, Message: "该机构服务已取消，不能暂停"},
			http.StatusConflict, "SUBSCRIPTION_CANCELLED"},
		{"order not voidable", &ConflictError{Sentinel: ErrOrderNotVoidable, Message: "订单已收款，不能作废；请先完成退款流程"},
			http.StatusConflict, "ORDER_NOT_VOIDABLE"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeInstitutionError(recorder, item.err)
			if recorder.Code != item.status {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, item.status, recorder.Body.String())
			}
			var payload struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if payload.Code != item.code || payload.Message == "" {
				t.Fatalf("code/message = %q/%q, want code %q", payload.Code, payload.Message, item.code)
			}
		})
	}

	// 内部扣量接口：暂停/取消必须是 409 + 固定错误码。
	for _, item := range []struct {
		err  error
		code string
	}{
		{ErrSubscriptionPaused, "SUBSCRIPTION_PAUSED"},
		{ErrSubscriptionCancelled, "SUBSCRIPTION_CANCELLED"},
	} {
		recorder := httptest.NewRecorder()
		writeInternalBillingError(recorder, item.err)
		if recorder.Code != http.StatusConflict {
			t.Fatalf("internal status = %d, want 409", recorder.Code)
		}
		if !strings.Contains(recorder.Body.String(), item.code) {
			t.Fatalf("internal body = %s, want code %s", recorder.Body.String(), item.code)
		}
	}
}
