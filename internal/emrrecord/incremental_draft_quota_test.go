package emrrecord

import (
	"context"
	"errors"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// 实验增量草稿与正式草稿共用 emr:record:<id>:draft 幂等键。余额为 0 时，
// 已扣过的病历仍要能做增量更新（本次不会再扣量），未被拦在预检。
func TestIncrementalDraftSkipsQuotaWhenDraftAlreadyDeducted(t *testing.T) {
	fake := &fakeBilling{
		// 模拟余额 0：正常预检会返回额度不足。
		quotaErr: &billing.InsufficientEntitlementError{Unit: "record", Current: 0, Required: 1, Shortfall: 1},
		deductedKeys: map[string]struct{}{
			"emr:record:rec-1:draft": {},
		},
	}
	handler := newDraftTestHandler(fake, 2700, nil)

	if err := handler.ensureDraftQuotaForRecord(context.Background(), 7, 42, "rec-1"); err != nil {
		t.Fatalf("already-deducted record must skip quota precheck at zero balance, got %v", err)
	}
	if fake.isDeductedCalls != 1 {
		t.Fatalf("idempotency lookups = %d, want 1", fake.isDeductedCalls)
	}
	if fake.quotaCalls != 0 {
		t.Fatalf("already-deducted record must not run quota precheck, calls = %d", fake.quotaCalls)
	}
}

// 未扣过的病历（余额 0）仍必须被预检拦住，不能因为增量草稿入口是实验接口就放行。
func TestIncrementalDraftStillBlockedWhenDraftNotDeducted(t *testing.T) {
	fake := &fakeBilling{
		quotaErr: &billing.InsufficientEntitlementError{Unit: "record", Current: 0, Required: 1, Shortfall: 1},
	}
	handler := newDraftTestHandler(fake, 2700, nil)

	err := handler.ensureDraftQuotaForRecord(context.Background(), 7, 42, "rec-2")
	if !errors.Is(err, ErrEMRQuotaExhausted) {
		t.Fatalf("expected ErrEMRQuotaExhausted, got %v", err)
	}
	var insufficient *billing.InsufficientEntitlementError
	if !errors.As(err, &insufficient) || insufficient.Unit != "record" {
		t.Fatalf("quota error should carry insufficient details, got %#v", err)
	}
	if fake.isDeductedCalls != 1 || fake.quotaCalls != 1 {
		t.Fatalf("lookups/prechecks = %d/%d, want 1/1", fake.isDeductedCalls, fake.quotaCalls)
	}
}

// 正式生成路径：同一接诊已有病历但没扣过（例如上次扣量失败）时，仍走预检，
// 余额不足继续拦截，符合"欠扣由下一次预检拦住"的既有策略。
func TestExistingDraftStillBlockedWhenNotDeducted(t *testing.T) {
	fake := &fakeBilling{
		quotaErr: &billing.InsufficientEntitlementError{Unit: "hour", Current: 0, Required: 0.75, Shortfall: 0.75},
	}
	handler := newDraftTestHandler(fake, 2700, nil)

	err := handler.ensureDraftQuotaForRecord(context.Background(), 7, 42, "rec-existing")
	if !errors.Is(err, ErrEMRQuotaExhausted) {
		t.Fatalf("expected ErrEMRQuotaExhausted, got %v", err)
	}
	if fake.quotaCalls != 1 {
		t.Fatalf("quota calls = %d, want 1", fake.quotaCalls)
	}
}

// 没有已知病历 id（首次生成）时不能跳过预检。
func TestDraftQuotaWithoutRecordIDRunsPrecheck(t *testing.T) {
	fake := &fakeBilling{
		quotaErr: &billing.InsufficientEntitlementError{Unit: "record", Current: 0, Required: 1, Shortfall: 1},
	}
	handler := newDraftTestHandler(fake, 2700, nil)

	err := handler.ensureDraftQuotaForRecord(context.Background(), 7, 42, "")
	if !errors.Is(err, ErrEMRQuotaExhausted) {
		t.Fatalf("expected ErrEMRQuotaExhausted, got %v", err)
	}
	if fake.isDeductedCalls != 0 || fake.quotaCalls != 1 {
		t.Fatalf("lookups/prechecks = %d/%d, want 0/1", fake.isDeductedCalls, fake.quotaCalls)
	}
}

// 只读幂等查询故障时回退到正常预检，不能把"查不到"当成"已扣过"直接放行。
func TestDraftQuotaFallsBackWhenIdempotencyLookupFails(t *testing.T) {
	fake := &fakeBilling{
		isDeductedErr: errors.New("billing db down"),
		quotaErr:      &billing.InsufficientEntitlementError{Unit: "record", Current: 0, Required: 1, Shortfall: 1},
	}
	handler := newDraftTestHandler(fake, 2700, nil)

	err := handler.ensureDraftQuotaForRecord(context.Background(), 7, 42, "rec-1")
	if !errors.Is(err, ErrEMRQuotaExhausted) {
		t.Fatalf("expected precheck rejection after lookup failure, got %v", err)
	}
	if fake.isDeductedCalls != 1 || fake.quotaCalls != 1 {
		t.Fatalf("lookups/prechecks = %d/%d, want 1/1", fake.isDeductedCalls, fake.quotaCalls)
	}
}

// 增量草稿与正式草稿必须使用完全相同的幂等键（含首尾空白归一化）。
func TestDraftIdempotencyKey(t *testing.T) {
	if got := draftIdempotencyKey(" rec-1 "); got != "emr:record:rec-1:draft" {
		t.Fatalf("draftIdempotencyKey() = %q", got)
	}
	input := draftDeductionInput(7, " rec-1 ", 2700)
	if input.IdempotencyKey != draftIdempotencyKey("rec-1") {
		t.Fatalf("deduction key = %q, want %q", input.IdempotencyKey, draftIdempotencyKey("rec-1"))
	}
}
