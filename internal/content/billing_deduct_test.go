package content

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// fakeContentBilling 是计费层的内存替身：记录预检/扣量入参，并按幂等键去重，
// 让单测可以在不依赖 PostgreSQL 与计费事务的情况下验证调用契约。
type fakeContentBilling struct {
	mu sync.Mutex

	quotaErr  error
	deductErr error

	quotaChecks []contentQuotaCheck
	deducts     []billing.DeductInput
	charged     map[string]int
}

type contentQuotaCheck struct {
	TenantID    int64
	ProductCode string
	Items       []billing.DeductItem
}

func newFakeContentBilling() *fakeContentBilling {
	return &fakeContentBilling{charged: make(map[string]int)}
}

func (f *fakeContentBilling) Deduct(_ context.Context, input billing.DeductInput) (*billing.DeductResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deducts = append(f.deducts, input)
	if f.deductErr != nil {
		return nil, f.deductErr
	}
	if f.charged[input.IdempotencyKey] > 0 {
		return nil, billing.ErrAlreadyDeducted
	}
	f.charged[input.IdempotencyKey]++
	return &billing.DeductResult{}, nil
}

func (f *fakeContentBilling) CheckQuota(_ context.Context, tenantID int64, productCode string, items []billing.DeductItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quotaChecks = append(f.quotaChecks, contentQuotaCheck{TenantID: tenantID, ProductCode: productCode, Items: items})
	return f.quotaErr
}

func (f *fakeContentBilling) deductCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deducts)
}

func (f *fakeContentBilling) chargedCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.charged[key]
}

func newContentTestService(bill billingService, seconds int64, durationErr error) *Service {
	return &Service{
		billing: bill,
		lookupRecordingDuration: func(context.Context, int64, int64) (int64, error) {
			return seconds, durationErr
		},
	}
}

func TestRecordingHours(t *testing.T) {
	cases := map[int64]float64{
		0:    0,
		1:    0, // 四舍五入到 2 位后为 0，调用方跳过 hour 扣量
		60:   0.02,
		5400: 1.5,
		7200: 2,
	}
	for seconds, want := range cases {
		if got := recordingHours(seconds); got != want {
			t.Fatalf("recordingHours(%d) = %v, want %v", seconds, got, want)
		}
	}
}

func TestContentDeductionInputs(t *testing.T) {
	t.Run("insight", func(t *testing.T) {
		input, ok := insightDeductionInput(7, 42, 5400)
		if !ok {
			t.Fatal("expected insight deduction input")
		}
		assertDeduction(t, input, contentBillingProductCode, contentBillingUnitHour, 1.5, "content:insight:42:analysis")
		if _, ok := insightDeductionInput(7, 42, 0); ok {
			t.Fatal("unknown duration should skip insight deduction")
		}
		if _, ok := insightDeductionInput(7, 0, 3600); ok {
			t.Fatal("missing recording id should skip insight deduction")
		}
	})

	t.Run("ai visibility", func(t *testing.T) {
		input := aiVisibilityDeductionInput(7, 9)
		assertDeduction(t, input, contentBillingProductCode, contentBillingUnitPrompt, 1, "content:ai-visibility:9:round")
	})

	t.Run("article", func(t *testing.T) {
		input, ok := articleDeductionInput(7, 11)
		if !ok {
			t.Fatal("expected article deduction input")
		}
		assertDeduction(t, input, contentBillingProductCode, contentBillingUnitRecord, 1, "content:article:11:deliver")
		if _, ok := articleDeductionInput(7, 0); ok {
			t.Fatal("missing article id should skip article deduction")
		}
	})

	t.Run("single image", func(t *testing.T) {
		input, ok := singleImageDeductionInput(7, 13)
		if !ok {
			t.Fatal("expected single image deduction input")
		}
		assertDeduction(t, input, contentBillingProductCode, contentBillingUnitShot, 1, "content:image:13:deliver")
	})

	t.Run("image slot", func(t *testing.T) {
		input, ok := imageSlotDeductionInput(7, 13, 2)
		if !ok {
			t.Fatal("expected image slot deduction input")
		}
		assertDeduction(t, input, contentBillingProductCode, contentBillingUnitShot, 1, "content:image:13-2:deliver")
		if _, ok := imageSlotDeductionInput(7, 13, -1); ok {
			t.Fatal("invalid image slot should skip deduction")
		}
	})
}

func assertDeduction(t *testing.T, input billing.DeductInput, product, unit string, amount float64, key string) {
	t.Helper()
	if input.TenantID != 7 || input.ProductCode != product {
		t.Fatalf("unexpected tenant/product: %#v", input)
	}
	if len(input.Items) != 1 || input.Items[0].Unit != unit || input.Items[0].Amount != amount {
		t.Fatalf("unexpected items: %#v", input.Items)
	}
	if input.IdempotencyKey != key {
		t.Fatalf("idempotency key = %q, want %q", input.IdempotencyKey, key)
	}
}

func TestEnsureContentQuota(t *testing.T) {
	cases := []struct {
		name string
		unit string
	}{
		{name: "ai visibility prompt", unit: contentBillingUnitPrompt},
		{name: "article record", unit: contentBillingUnitRecord},
		{name: "image shot", unit: contentBillingUnitShot},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeContentBilling()
			svc := newContentTestService(fake, 0, nil)
			if err := svc.ensureContentQuota(context.Background(), 7, tc.unit, 1); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(fake.quotaChecks) != 1 {
				t.Fatalf("quota check calls = %d, want 1", len(fake.quotaChecks))
			}
			check := fake.quotaChecks[0]
			if check.TenantID != 7 || check.ProductCode != contentBillingProductCode {
				t.Fatalf("unexpected quota check target: %#v", check)
			}
			if len(check.Items) != 1 || check.Items[0].Unit != tc.unit || check.Items[0].Amount != 1 {
				t.Fatalf("unexpected quota check items: %#v", check.Items)
			}
		})
	}

	t.Run("insufficient entitlement blocks generation", func(t *testing.T) {
		fake := newFakeContentBilling()
		fake.quotaErr = &billing.InsufficientEntitlementError{Unit: contentBillingUnitPrompt, Current: 0, Required: 1, Shortfall: 1}
		svc := newContentTestService(fake, 0, nil)

		err := svc.ensureContentQuota(context.Background(), 7, contentBillingUnitPrompt, 1)
		if !errors.Is(err, ErrContentQuotaExhausted) {
			t.Fatalf("expected ErrContentQuotaExhausted, got %v", err)
		}
		var insufficient *billing.InsufficientEntitlementError
		if !errors.As(err, &insufficient) || insufficient.Unit != contentBillingUnitPrompt {
			t.Fatalf("quota error should carry insufficient details, got %#v", err)
		}
		if fake.deductCount() != 0 {
			t.Fatalf("precheck must not deduct, got %d deduct calls", fake.deductCount())
		}
	})

	t.Run("billing failure fails open", func(t *testing.T) {
		fake := newFakeContentBilling()
		fake.quotaErr = errors.New("billing unavailable")
		svc := newContentTestService(fake, 0, nil)

		if err := svc.ensureContentQuota(context.Background(), 7, contentBillingUnitRecord, 1); err != nil {
			t.Fatalf("non-entitlement precheck errors must fail open, got %v", err)
		}
	})

	t.Run("billing not wired fails open", func(t *testing.T) {
		svc := newContentTestService(nil, 0, nil)
		if err := svc.ensureContentQuota(context.Background(), 7, contentBillingUnitRecord, 1); err != nil {
			t.Fatalf("missing billing must fail open, got %v", err)
		}
	})
}

func TestChargeInsightDeductsHourOnce(t *testing.T) {
	fake := newFakeContentBilling()
	svc := newContentTestService(fake, 5400, nil)

	svc.chargeInsight(context.Background(), 7, 42)
	// 重复触发（worker 重试/手动补齐）应命中幂等键，只扣一次。
	svc.chargeInsight(context.Background(), 7, 42)

	if fake.deductCount() != 2 {
		t.Fatalf("deduct attempts = %d, want 2", fake.deductCount())
	}
	if got := fake.chargedCount("content:insight:42:analysis"); got != 1 {
		t.Fatalf("successful deductions = %d, want 1 (idempotent)", got)
	}
	input := fake.deducts[0]
	assertDeduction(t, input, contentBillingProductCode, contentBillingUnitHour, 1.5, "content:insight:42:analysis")
}

func TestChargeInsightSkipsUnknownDuration(t *testing.T) {
	fake := newFakeContentBilling()
	svc := newContentTestService(fake, 0, errors.New("recording not found"))

	svc.chargeInsight(context.Background(), 7, 42)
	if fake.deductCount() != 0 {
		t.Fatalf("unknown duration should skip deduction, got %d", fake.deductCount())
	}
}

func TestChargeContentDeliveriesAreIdempotent(t *testing.T) {
	fake := newFakeContentBilling()
	svc := newContentTestService(fake, 0, nil)

	deliveries := []struct {
		name string
		key  string
		run  func()
	}{
		{name: "ai visibility", key: "content:ai-visibility:9:round", run: func() { svc.chargeAIVisibility(context.Background(), 7, 9) }},
		{name: "article", key: "content:article:11:deliver", run: func() { svc.chargeArticle(context.Background(), 7, 11) }},
		{name: "single image", key: "content:image:13:deliver", run: func() { svc.chargeSingleImage(context.Background(), 7, 13) }},
		{name: "image slot", key: "content:image:13-2:deliver", run: func() { svc.chargeImageSlot(context.Background(), 7, 13, 2) }},
	}
	for _, delivery := range deliveries {
		t.Run(delivery.name, func(t *testing.T) {
			delivery.run()
			delivery.run()
			if got := fake.chargedCount(delivery.key); got != 1 {
				t.Fatalf("successful deductions for %s = %d, want 1", delivery.key, got)
			}
		})
	}
}

func TestChargeContentFailureDoesNotPanic(t *testing.T) {
	fake := newFakeContentBilling()
	fake.deductErr = errors.New("billing transaction failed")
	svc := newContentTestService(fake, 3600, nil)

	// 扣量失败只告警，不返回错误、不影响已经落库的业务结果。
	svc.chargeInsight(context.Background(), 7, 42)
	svc.chargeAIVisibility(context.Background(), 7, 9)
	svc.chargeArticle(context.Background(), 7, 11)
	svc.chargeSingleImage(context.Background(), 7, 13)
	svc.chargeImageSlot(context.Background(), 7, 13, 2)

	if fake.deductCount() != 5 {
		t.Fatalf("deduct attempts = %d, want 5", fake.deductCount())
	}
}

func TestWriteContentErrorMapsQuotaExhausted(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeContentError(recorder, &contentQuotaExhaustedError{
		insufficient: &billing.InsufficientEntitlementError{Unit: contentBillingUnitPrompt, Current: 0, Required: 1, Shortfall: 1},
	})

	if recorder.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", recorder.Code)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != billing.CodeInsufficientEntitlement {
		t.Fatalf("code = %q, want %q", body.Code, billing.CodeInsufficientEntitlement)
	}
	if body.Message != billing.MessageInsufficientEntitlement {
		t.Fatalf("message = %q", body.Message)
	}
}

func TestContentUseBillingHandlesNil(t *testing.T) {
	(*Service)(nil).UseBilling(nil)
	if got := wrapBillingService(nil); got != nil {
		t.Fatalf("wrapping nil billing must stay nil, got %#v", got)
	}
}
