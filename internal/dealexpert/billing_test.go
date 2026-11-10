package dealexpert

// 赢客专家计费接入的单元测试：用内存 fake 计费替身验证交付扣量契约、
// 幂等、时长未知跳过，以及调模型前软闸门的拒绝/fail-open 行为。
// 不依赖 PostgreSQL、计费事务或模型网关。

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/pkg/llmgateway"
)

// fakeDealExpertBilling 是计费层的内存替身：记录预检/扣量入参，并按幂等键去重。
type fakeDealExpertBilling struct {
	mu sync.Mutex

	quotaErr  error
	deductErr error

	quotaChecks  []billing.DeductItem
	quotaCalls   int
	quotaTenant  int64
	quotaProduct string
	deducts      []billing.DeductInput
	charged      map[string]int
}

func newFakeDealExpertBilling() *fakeDealExpertBilling {
	return &fakeDealExpertBilling{charged: make(map[string]int)}
}

func (f *fakeDealExpertBilling) Deduct(_ context.Context, input billing.DeductInput) (*billing.DeductResult, error) {
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

func (f *fakeDealExpertBilling) CheckQuota(_ context.Context, tenantID int64, productCode string, items []billing.DeductItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quotaCalls++
	f.quotaTenant = tenantID
	f.quotaProduct = productCode
	f.quotaChecks = append(f.quotaChecks, items...)
	return f.quotaErr
}

func (f *fakeDealExpertBilling) deductCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deducts)
}

func (f *fakeDealExpertBilling) chargedCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.charged[key]
}

func (f *fakeDealExpertBilling) lastDeduct(t *testing.T) billing.DeductInput {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deducts) == 0 {
		t.Fatal("expected a deduction call")
	}
	return f.deducts[len(f.deducts)-1]
}

func newDealExpertTestGenerator(bill billingService, seconds int64, durationErr error) *ConsultationAnalysisGenerator {
	return &ConsultationAnalysisGenerator{
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

func TestAnalysisDeductionInput(t *testing.T) {
	input, ok := analysisDeductionInput(7, 42, 5400)
	if !ok {
		t.Fatal("expected deduction input for known duration")
	}
	if input.TenantID != 7 || input.ProductCode != "yingke" {
		t.Fatalf("unexpected tenant/product: %+v", input)
	}
	if len(input.Items) != 1 || input.Items[0].Unit != "hour" || input.Items[0].Amount != 1.5 {
		t.Fatalf("unexpected items: %+v", input.Items)
	}
	if input.IdempotencyKey != "deal:recording:42:analysis" {
		t.Fatalf("unexpected idempotency key: %s", input.IdempotencyKey)
	}
	if input.Reason != "赢客专家-成交分析" {
		t.Fatalf("unexpected reason: %s", input.Reason)
	}
	if _, ok := analysisDeductionInput(7, 42, 0); ok {
		t.Fatal("unknown duration should skip deduction")
	}
	if _, ok := analysisDeductionInput(7, 0, 3600); ok {
		t.Fatal("missing recording id should skip deduction")
	}
}

func TestChargeAnalysisDeductsHourOnce(t *testing.T) {
	fake := newFakeDealExpertBilling()
	gen := newDealExpertTestGenerator(fake, 5400, nil)
	ctx := context.Background()

	gen.chargeAnalysis(ctx, 7, 42)
	// 同一录音重复生成（worker 重投/重试）必须命中幂等键，只生效一次。
	gen.chargeAnalysis(ctx, 7, 42)

	if got := fake.deductCount(); got != 2 {
		t.Fatalf("deduct calls = %d, want 2 (second is idempotent)", got)
	}
	if got := fake.chargedCount("deal:recording:42:analysis"); got != 1 {
		t.Fatalf("effective deductions = %d, want 1", got)
	}
	last := fake.lastDeduct(t)
	if last.IdempotencyKey != "deal:recording:42:analysis" || last.Items[0].Amount != 1.5 {
		t.Fatalf("unexpected deduct input: %+v", last)
	}
}

func TestChargeAnalysisSkipsUnknownDuration(t *testing.T) {
	t.Run("zero duration", func(t *testing.T) {
		fake := newFakeDealExpertBilling()
		newDealExpertTestGenerator(fake, 0, nil).chargeAnalysis(context.Background(), 7, 42)
		if got := fake.deductCount(); got != 0 {
			t.Fatalf("deduct calls = %d, want 0", got)
		}
	})
	t.Run("duration lookup error", func(t *testing.T) {
		fake := newFakeDealExpertBilling()
		newDealExpertTestGenerator(fake, 0, errors.New("recording not found")).
			chargeAnalysis(context.Background(), 7, 42)
		if got := fake.deductCount(); got != 0 {
			t.Fatalf("deduct calls = %d, want 0", got)
		}
	})
	t.Run("billing not wired", func(t *testing.T) {
		// 未接计费时 charge 直接返回，不 panic、不扣量。
		newDealExpertTestGenerator(nil, 5400, nil).chargeAnalysis(context.Background(), 7, 42)
	})
}

func TestAnalysisQuotaGateBlocksLLMAndDeduction(t *testing.T) {
	fake := newFakeDealExpertBilling()
	fake.quotaErr = &billing.InsufficientEntitlementError{Unit: "hour", Current: 0, Required: 1.5, Shortfall: 1.5}
	gen := newDealExpertTestGenerator(fake, 5400, nil)

	llmCalled := false
	_, err := gen.callLLMWithQuotaGate(context.Background(), 7, 42, func() (*llmgateway.TextInferenceResponse, error) {
		llmCalled = true
		return &llmgateway.TextInferenceResponse{}, nil
	})

	if err == nil || !errors.Is(err, ErrDealExpertQuotaExhausted) {
		t.Fatalf("want ErrDealExpertQuotaExhausted, got %v", err)
	}
	if !errors.Is(err, billing.ErrInsufficientEntitlement) {
		t.Fatalf("want error to unwrap to ErrInsufficientEntitlement, got %v", err)
	}
	if llmCalled {
		t.Fatal("LLM must not be called when quota is exhausted")
	}
	if got := fake.deductCount(); got != 0 {
		t.Fatalf("deduction calls = %d, want 0", got)
	}
	if fake.quotaCalls != 1 || len(fake.quotaChecks) != 1 {
		t.Fatalf("unexpected quota checks: calls=%d items=%+v", fake.quotaCalls, fake.quotaChecks)
	}
	if fake.quotaTenant != 7 || fake.quotaProduct != "yingke" {
		t.Fatalf("unexpected quota tenant/product: %d/%s", fake.quotaTenant, fake.quotaProduct)
	}
	if fake.quotaChecks[0].Unit != "hour" || fake.quotaChecks[0].Amount != 1.5 {
		t.Fatalf("unexpected quota check item: %+v", fake.quotaChecks[0])
	}
}

func TestAnalysisQuotaGateFailOpen(t *testing.T) {
	fake := newFakeDealExpertBilling()
	fake.quotaErr = errors.New("billing service unavailable")
	gen := newDealExpertTestGenerator(fake, 5400, nil)

	llmCalled := false
	resp, err := gen.callLLMWithQuotaGate(context.Background(), 7, 42, func() (*llmgateway.TextInferenceResponse, error) {
		llmCalled = true
		return &llmgateway.TextInferenceResponse{RequestID: "req-1"}, nil
	})

	if err != nil {
		t.Fatalf("fail-open should allow analysis, got %v", err)
	}
	if !llmCalled || resp == nil || resp.RequestID != "req-1" {
		t.Fatalf("expected LLM to be called, got called=%v resp=%+v", llmCalled, resp)
	}
	if got := fake.deductCount(); got != 0 {
		t.Fatalf("deduction calls = %d, want 0", got)
	}
}

func TestAnalysisQuotaGateSkipsUnknownDuration(t *testing.T) {
	fake := newFakeDealExpertBilling()
	gen := newDealExpertTestGenerator(fake, 0, errors.New("recording not found"))

	llmCalled := false
	if _, err := gen.callLLMWithQuotaGate(context.Background(), 7, 42, func() (*llmgateway.TextInferenceResponse, error) {
		llmCalled = true
		return &llmgateway.TextInferenceResponse{}, nil
	}); err != nil {
		t.Fatalf("unknown duration should fail-open, got %v", err)
	}
	if !llmCalled {
		t.Fatal("expected LLM to be called when duration is unknown")
	}
	if fake.quotaCalls != 0 {
		t.Fatalf("quota checks = %d, want 0", fake.quotaCalls)
	}
}

func TestChargeAnalysisDeductionFailureKeepsBusinessResult(t *testing.T) {
	fake := newFakeDealExpertBilling()
	fake.deductErr = errors.New("billing ledger unavailable")
	gen := newDealExpertTestGenerator(fake, 5400, nil)

	// 分析已经落库；扣量失败只能告警，不能阻断/回滚业务结果。
	delivered := true
	gen.chargeAnalysis(context.Background(), 7, 42)
	if !delivered {
		t.Fatal("business result must be kept when deduction fails")
	}
	if got := fake.deductCount(); got != 1 {
		t.Fatalf("deduct attempts = %d, want 1", got)
	}
}

func TestUseBillingHandlesNil(t *testing.T) {
	(*ConsultationAnalysisGenerator)(nil).UseBilling(nil)

	gen := &ConsultationAnalysisGenerator{}
	gen.UseBilling(nil)
	if gen.billing != nil {
		t.Fatal("nil *billing.Service must not become a non-nil interface")
	}
}
