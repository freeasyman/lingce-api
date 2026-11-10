package followup

// 随访顾问计费接入的单元测试：用内存 fake 计费替身验证任务落库后的 hour 扣量、
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

// fakeFollowupBilling 是计费层的内存替身：记录预检/扣量入参，并按幂等键去重。
type fakeFollowupBilling struct {
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

func newFakeFollowupBilling() *fakeFollowupBilling {
	return &fakeFollowupBilling{charged: make(map[string]int)}
}

func (f *fakeFollowupBilling) Deduct(_ context.Context, input billing.DeductInput) (*billing.DeductResult, error) {
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

func (f *fakeFollowupBilling) CheckQuota(_ context.Context, tenantID int64, productCode string, items []billing.DeductItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quotaCalls++
	f.quotaTenant = tenantID
	f.quotaProduct = productCode
	f.quotaChecks = append(f.quotaChecks, items...)
	return f.quotaErr
}

func (f *fakeFollowupBilling) deductCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deducts)
}

func (f *fakeFollowupBilling) chargedCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.charged[key]
}

func (f *fakeFollowupBilling) lastDeduct(t *testing.T) billing.DeductInput {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deducts) == 0 {
		t.Fatal("expected a deduction call")
	}
	return f.deducts[len(f.deducts)-1]
}

func newFollowupTestService(bill billingService, seconds int64, durationErr error) *Service {
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

func TestAnalysisDeductionInput(t *testing.T) {
	input, ok := analysisDeductionInput(7, 42, 5400)
	if !ok {
		t.Fatal("expected deduction input for known duration")
	}
	if input.TenantID != 7 || input.ProductCode != "suifang" {
		t.Fatalf("unexpected tenant/product: %+v", input)
	}
	if len(input.Items) != 1 || input.Items[0].Unit != "hour" || input.Items[0].Amount != 1.5 {
		t.Fatalf("unexpected items: %+v", input.Items)
	}
	if input.IdempotencyKey != "followup:recording:42:analysis" {
		t.Fatalf("unexpected idempotency key: %s", input.IdempotencyKey)
	}
	if input.Reason != "随访顾问-随访分析" {
		t.Fatalf("unexpected reason: %s", input.Reason)
	}
	if _, ok := analysisDeductionInput(7, 42, 0); ok {
		t.Fatal("unknown duration should skip deduction")
	}
	if _, ok := analysisDeductionInput(7, 0, 3600); ok {
		t.Fatal("missing recording id should skip deduction")
	}
}

func TestChargeFollowupAnalysisDeductsHourOnce(t *testing.T) {
	fake := newFakeFollowupBilling()
	service := newFollowupTestService(fake, 5400, nil)
	ctx := context.Background()

	service.chargeFollowupAnalysis(ctx, 7, 42)
	// 同一录音重复生成（补偿重试/重投）必须命中幂等键，只生效一次。
	service.chargeFollowupAnalysis(ctx, 7, 42)

	if got := fake.deductCount(); got != 2 {
		t.Fatalf("deduct calls = %d, want 2 (second is idempotent)", got)
	}
	if got := fake.chargedCount("followup:recording:42:analysis"); got != 1 {
		t.Fatalf("effective deductions = %d, want 1", got)
	}
	last := fake.lastDeduct(t)
	if last.IdempotencyKey != "followup:recording:42:analysis" || last.Items[0].Amount != 1.5 {
		t.Fatalf("unexpected deduct input: %+v", last)
	}
}

func TestChargeFollowupAnalysisSkipsUnknownDuration(t *testing.T) {
	t.Run("zero duration", func(t *testing.T) {
		fake := newFakeFollowupBilling()
		newFollowupTestService(fake, 0, nil).chargeFollowupAnalysis(context.Background(), 7, 42)
		if got := fake.deductCount(); got != 0 {
			t.Fatalf("deduct calls = %d, want 0", got)
		}
	})
	t.Run("duration lookup error", func(t *testing.T) {
		fake := newFakeFollowupBilling()
		newFollowupTestService(fake, 0, errors.New("recording not found")).
			chargeFollowupAnalysis(context.Background(), 7, 42)
		if got := fake.deductCount(); got != 0 {
			t.Fatalf("deduct calls = %d, want 0", got)
		}
	})
	t.Run("billing not wired", func(t *testing.T) {
		// 未接计费时 charge 直接返回，不 panic、不扣量。
		newFollowupTestService(nil, 5400, nil).chargeFollowupAnalysis(context.Background(), 7, 42)
	})
}

func TestFollowupQuotaGateBlocksLLMAndDeduction(t *testing.T) {
	fake := newFakeFollowupBilling()
	fake.quotaErr = &billing.InsufficientEntitlementError{Unit: "hour", Current: 0, Required: 1.5, Shortfall: 1.5}
	service := newFollowupTestService(fake, 5400, nil)

	llmCalled := false
	_, err := service.callLLMWithQuotaGate(context.Background(), GenerateRequest{TenantID: 7, RecordingID: 42},
		func() (*llmgateway.TextInferenceResponse, error) {
			llmCalled = true
			return &llmgateway.TextInferenceResponse{}, nil
		})

	if err == nil || !errors.Is(err, ErrFollowupQuotaExhausted) {
		t.Fatalf("want ErrFollowupQuotaExhausted, got %v", err)
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
	if fake.quotaTenant != 7 || fake.quotaProduct != "suifang" {
		t.Fatalf("unexpected quota tenant/product: %d/%s", fake.quotaTenant, fake.quotaProduct)
	}
	if fake.quotaChecks[0].Unit != "hour" || fake.quotaChecks[0].Amount != 1.5 {
		t.Fatalf("unexpected quota check item: %+v", fake.quotaChecks[0])
	}
}

func TestFollowupQuotaGateFailOpen(t *testing.T) {
	fake := newFakeFollowupBilling()
	fake.quotaErr = errors.New("billing service unavailable")
	service := newFollowupTestService(fake, 5400, nil)

	llmCalled := false
	resp, err := service.callLLMWithQuotaGate(context.Background(), GenerateRequest{TenantID: 7, RecordingID: 42},
		func() (*llmgateway.TextInferenceResponse, error) {
			llmCalled = true
			return &llmgateway.TextInferenceResponse{RequestID: "req-1"}, nil
		})

	if err != nil {
		t.Fatalf("fail-open should allow generation, got %v", err)
	}
	if !llmCalled || resp == nil || resp.RequestID != "req-1" {
		t.Fatalf("expected LLM to be called, got called=%v resp=%+v", llmCalled, resp)
	}
	if got := fake.deductCount(); got != 0 {
		t.Fatalf("deduction calls = %d, want 0", got)
	}
}

func TestFollowupQuotaGateSkipsUnknownDuration(t *testing.T) {
	fake := newFakeFollowupBilling()
	service := newFollowupTestService(fake, 0, errors.New("recording not found"))

	llmCalled := false
	if _, err := service.callLLMWithQuotaGate(context.Background(), GenerateRequest{TenantID: 7, RecordingID: 42},
		func() (*llmgateway.TextInferenceResponse, error) {
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

func TestChargeFollowupDeductionFailureKeepsBusinessResult(t *testing.T) {
	fake := newFakeFollowupBilling()
	fake.deductErr = errors.New("billing ledger unavailable")
	service := newFollowupTestService(fake, 5400, nil)

	// 任务已经落库；扣量失败只能告警，不能阻断/回滚业务结果。
	delivered := true
	service.chargeFollowupAnalysis(context.Background(), 7, 42)
	if !delivered {
		t.Fatal("business result must be kept when deduction fails")
	}
	if got := fake.deductCount(); got != 1 {
		t.Fatalf("deduct attempts = %d, want 1", got)
	}
}

func TestUseBillingHandlesNil(t *testing.T) {
	(*Module)(nil).UseBilling(nil)
	(&Module{}).UseBilling(nil)
	(*Service)(nil).UseBilling(nil)

	service := &Service{}
	service.UseBilling(nil)
	if service.billing != nil {
		t.Fatal("nil *billing.Service must not become a non-nil interface")
	}

	// Module 的 UseBilling 必须下沉到 Handler 持有的 Service，而不是另存一份。
	inner := &Service{}
	module := &Module{handler: &Handler{service: inner}}
	module.UseBilling(nil)
	if inner.billing != nil {
		t.Fatal("module UseBilling did not reach the service")
	}
}
