package complianceguard

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

// fakeComplianceBilling 是计费层的内存替身：记录预检/扣量入参，并按幂等键去重，
// 让单测可以在不依赖 PostgreSQL 与计费事务的情况下验证调用契约。
type fakeComplianceBilling struct {
	mu sync.Mutex

	quotaErr  error
	deductErr error

	quotaChecks []complianceQuotaCheck
	deducts     []billing.DeductInput
	charged     map[string]int
}

type complianceQuotaCheck struct {
	TenantID    int64
	ProductCode string
	Items       []billing.DeductItem
}

func newFakeComplianceBilling() *fakeComplianceBilling {
	return &fakeComplianceBilling{charged: make(map[string]int)}
}

func (f *fakeComplianceBilling) Deduct(_ context.Context, input billing.DeductInput) (*billing.DeductResult, error) {
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

func (f *fakeComplianceBilling) CheckQuota(_ context.Context, tenantID int64, productCode string, items []billing.DeductItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quotaChecks = append(f.quotaChecks, complianceQuotaCheck{TenantID: tenantID, ProductCode: productCode, Items: items})
	return f.quotaErr
}

func (f *fakeComplianceBilling) deductCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deducts)
}

func (f *fakeComplianceBilling) chargedCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.charged[key]
}

func newComplianceTestService(bill billingService, seconds int64, durationErr error) *Service {
	return &Service{
		billing: bill,
		lookupRecordingDuration: func(context.Context, int64, int64) (int64, error) {
			return seconds, durationErr
		},
	}
}

func TestCommunicationCheckDeductionInput(t *testing.T) {
	cases := []struct {
		name    string
		seconds int64
		wantOK  bool
		wantHrs float64
	}{
		{name: "one and a half hours", seconds: 5400, wantOK: true, wantHrs: 1.5},
		{name: "rounds to two decimals", seconds: 60, wantOK: true, wantHrs: 0.02},
		{name: "two hours", seconds: 7200, wantOK: true, wantHrs: 2},
		{name: "unknown duration", seconds: 0, wantOK: false},
		{name: "rounds down to zero", seconds: 1, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, ok := communicationCheckDeductionInput(7, "run-1", tc.seconds)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if input.TenantID != 7 || input.ProductCode != complianceBillingProductCode {
				t.Fatalf("unexpected tenant/product: %#v", input)
			}
			if len(input.Items) != 1 || input.Items[0].Unit != complianceBillingUnitHour || input.Items[0].Amount != tc.wantHrs {
				t.Fatalf("unexpected items: %#v", input.Items)
			}
			if input.IdempotencyKey != "compliance:run:run-1:check" {
				t.Fatalf("idempotency key = %q", input.IdempotencyKey)
			}
		})
	}

	if _, ok := communicationCheckDeductionInput(7, "  ", 3600); ok {
		t.Fatal("empty run id should skip deduction")
	}
}

func TestEnsureCommunicationQuota(t *testing.T) {
	t.Run("insufficient entitlement blocks check with quota error", func(t *testing.T) {
		fake := newFakeComplianceBilling()
		fake.quotaErr = &billing.InsufficientEntitlementError{Unit: complianceBillingUnitHour, Current: 0.5, Required: 1, Shortfall: 0.5}
		svc := newComplianceTestService(fake, 3600, nil)

		err := svc.ensureCommunicationQuota(context.Background(), 7, 42)
		if !errors.Is(err, ErrComplianceQuotaExhausted) {
			t.Fatalf("expected ErrComplianceQuotaExhausted, got %v", err)
		}
		var insufficient *billing.InsufficientEntitlementError
		if !errors.As(err, &insufficient) || insufficient.Unit != complianceBillingUnitHour {
			t.Fatalf("quota error should carry insufficient details, got %#v", err)
		}
		if fake.deductCount() != 0 {
			t.Fatalf("precheck must not deduct, got %d deduct calls", fake.deductCount())
		}
	})

	t.Run("insufficient bare sentinel still maps to quota error", func(t *testing.T) {
		fake := newFakeComplianceBilling()
		fake.quotaErr = billing.ErrInsufficientEntitlement
		svc := newComplianceTestService(fake, 3600, nil)

		if err := svc.ensureCommunicationQuota(context.Background(), 7, 42); !errors.Is(err, ErrComplianceQuotaExhausted) {
			t.Fatalf("expected ErrComplianceQuotaExhausted, got %v", err)
		}
	})

	t.Run("billing failure fails open", func(t *testing.T) {
		fake := newFakeComplianceBilling()
		fake.quotaErr = errors.New("billing unavailable")
		svc := newComplianceTestService(fake, 3600, nil)

		if err := svc.ensureCommunicationQuota(context.Background(), 7, 42); err != nil {
			t.Fatalf("non-entitlement precheck errors must fail open, got %v", err)
		}
	})

	t.Run("billing not wired fails open", func(t *testing.T) {
		svc := newComplianceTestService(nil, 3600, nil)
		if err := svc.ensureCommunicationQuota(context.Background(), 7, 42); err != nil {
			t.Fatalf("missing billing must fail open, got %v", err)
		}
	})

	t.Run("unknown duration fails open without quota call", func(t *testing.T) {
		fake := newFakeComplianceBilling()
		svc := newComplianceTestService(fake, 0, errors.New("recording not found"))

		if err := svc.ensureCommunicationQuota(context.Background(), 7, 42); err != nil {
			t.Fatalf("unknown duration must fail open, got %v", err)
		}
		if len(fake.quotaChecks) != 0 {
			t.Fatalf("unknown duration should skip quota check, got %d", len(fake.quotaChecks))
		}
	})

	t.Run("enough quota checks the recording hours", func(t *testing.T) {
		fake := newFakeComplianceBilling()
		svc := newComplianceTestService(fake, 5400, nil)

		if err := svc.ensureCommunicationQuota(context.Background(), 7, 42); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(fake.quotaChecks) != 1 {
			t.Fatalf("quota check calls = %d, want 1", len(fake.quotaChecks))
		}
		check := fake.quotaChecks[0]
		if check.TenantID != 7 || check.ProductCode != complianceBillingProductCode {
			t.Fatalf("unexpected quota check target: %#v", check)
		}
		if len(check.Items) != 1 || check.Items[0].Unit != complianceBillingUnitHour || check.Items[0].Amount != 1.5 {
			t.Fatalf("unexpected quota check items: %#v", check.Items)
		}
	})
}

func TestChargeCommunicationCheckDeductsOnce(t *testing.T) {
	fake := newFakeComplianceBilling()
	svc := newComplianceTestService(fake, 5400, nil)

	svc.chargeCommunicationCheck(context.Background(), 7, 42, "run-1")
	// 重复触发（worker 重试/重复回调）应命中幂等键，只扣一次。
	svc.chargeCommunicationCheck(context.Background(), 7, 42, "run-1")

	if fake.deductCount() != 2 {
		t.Fatalf("deduct calls = %d, want 2 attempts", fake.deductCount())
	}
	if got := fake.chargedCount("compliance:run:run-1:check"); got != 1 {
		t.Fatalf("successful deductions = %d, want 1 (idempotent)", got)
	}
	input := fake.deducts[0]
	if input.TenantID != 7 || input.ProductCode != complianceBillingProductCode {
		t.Fatalf("unexpected tenant/product: %#v", input)
	}
	if len(input.Items) != 1 || input.Items[0].Unit != complianceBillingUnitHour || input.Items[0].Amount != 1.5 {
		t.Fatalf("unexpected deduction items: %#v", input.Items)
	}
}

func TestChargeCommunicationCheckSkipsUnknownDuration(t *testing.T) {
	fake := newFakeComplianceBilling()
	svc := newComplianceTestService(fake, 0, errors.New("recording not found"))

	svc.chargeCommunicationCheck(context.Background(), 7, 42, "run-1")
	if fake.deductCount() != 0 {
		t.Fatalf("unknown duration should skip deduction, got %d", fake.deductCount())
	}
}

func TestChargeCommunicationCheckFailureDoesNotPanic(t *testing.T) {
	fake := newFakeComplianceBilling()
	fake.deductErr = errors.New("billing transaction failed")
	svc := newComplianceTestService(fake, 3600, nil)

	// 扣量失败只告警，不返回错误、不影响已经落库的检查结果。
	svc.chargeCommunicationCheck(context.Background(), 7, 42, "run-1")
	if fake.deductCount() != 1 {
		t.Fatalf("deduct attempts = %d, want 1", fake.deductCount())
	}
}

func TestWriteComplianceQuotaExhausted(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeComplianceQuotaExhausted(recorder, &complianceQuotaExhaustedError{
		insufficient: &billing.InsufficientEntitlementError{Unit: complianceBillingUnitHour, Current: 0, Required: 1, Shortfall: 1},
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
	if body.Code != complianceQuotaExhaustedCode {
		t.Fatalf("code = %q, want %q", body.Code, complianceQuotaExhaustedCode)
	}
	if body.Message != "额度已用完，请及时联系客服，加购增量包" {
		t.Fatalf("message = %q", body.Message)
	}
}

func TestUseBillingHandlesNil(t *testing.T) {
	(*Service)(nil).UseBilling(nil)
	if got := wrapBillingService(nil); got != nil {
		t.Fatalf("wrapping nil billing must stay nil, got %#v", got)
	}
}
