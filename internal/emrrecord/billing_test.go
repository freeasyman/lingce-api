package emrrecord

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/internal/emrquality"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeBilling 是 billingService 的内存实现：记录每次扣量与预检的入参，
// 让扣量口径（unit/amount/idempotency key）可以直接断言，不依赖数据库。
type fakeBilling struct {
	deductions   []billing.DeductInput
	quotaItems   []billing.DeductItem
	quotaTenant  int64
	quotaProduct string
	quotaCalls   int
	quotaErr     error
	deductErr    error
}

func (f *fakeBilling) Deduct(_ context.Context, input billing.DeductInput) (*billing.DeductResult, error) {
	f.deductions = append(f.deductions, input)
	if f.deductErr != nil {
		return nil, f.deductErr
	}
	return &billing.DeductResult{}, nil
}

func (f *fakeBilling) CheckQuota(_ context.Context, tenantID int64, productCode string, items []billing.DeductItem) error {
	f.quotaCalls++
	f.quotaTenant = tenantID
	f.quotaProduct = productCode
	f.quotaItems = append([]billing.DeductItem(nil), items...)
	return f.quotaErr
}

func newDraftTestHandler(fake *fakeBilling, seconds int64, durationErr error) *InternalHandler {
	return &InternalHandler{
		billing: fake,
		lookupDuration: func(context.Context, int64, int64) (int64, error) {
			return seconds, durationErr
		},
	}
}

func itemsByUnit(items []billing.DeductItem) map[string]float64 {
	result := make(map[string]float64, len(items))
	for _, item := range items {
		result[item.Unit] = item.Amount
	}
	return result
}

// 草稿生成成功：record 1 份 + hour 按录音时长（秒/3600，2 位小数）+ 固定幂等键。
func TestDraftGenerationDeductsRecordAndRecordingHours(t *testing.T) {
	fake := &fakeBilling{}
	handler := newDraftTestHandler(fake, 2700, nil) // 45 分钟 = 0.75 小时

	handler.chargeGeneratedDraft(context.Background(), EmrWorkerRequest{TenantID: 7, RecordingID: 42}, &Record{ID: "rec-1"})

	if len(fake.deductions) != 1 {
		t.Fatalf("deductions = %d, want 1", len(fake.deductions))
	}
	got := fake.deductions[0]
	if got.TenantID != 7 || got.ProductCode != "tingxie" {
		t.Fatalf("unexpected tenant/product: %+v", got)
	}
	if got.IdempotencyKey != "emr:record:rec-1:draft" {
		t.Fatalf("idempotency key = %q", got.IdempotencyKey)
	}
	items := itemsByUnit(got.Items)
	if len(items) != 2 || items["record"] != 1 || items["hour"] != 0.75 {
		t.Fatalf("items = %#v, want record:1 + hour:0.75", got.Items)
	}
}

func TestRecordingHoursRoundsToTwoDecimals(t *testing.T) {
	cases := []struct {
		seconds int64
		want    float64
	}{
		{seconds: -5, want: 0},
		{seconds: 0, want: 0},
		{seconds: 8, want: 0},       // 0.002 小时，四舍五入到 0.00
		{seconds: 600, want: 0.17},  // 10 分钟
		{seconds: 2700, want: 0.75}, // 45 分钟
		{seconds: 3600, want: 1},    // 1 小时
		{seconds: 5430, want: 1.51}, // 90.5 分钟
		{seconds: 7200, want: 2},    // 2 小时
	}
	for _, tc := range cases {
		if got := recordingHours(tc.seconds); got != tc.want {
			t.Fatalf("recordingHours(%d) = %v, want %v", tc.seconds, got, tc.want)
		}
	}
}

// 时长未知时只扣 record，不产生 0 金额的 hour 计量项（计费层会拒绝）。
func TestDraftGenerationSkipsHourWhenDurationUnknown(t *testing.T) {
	fake := &fakeBilling{}
	handler := newDraftTestHandler(fake, 0, errors.New("recording missing"))

	handler.chargeGeneratedDraft(context.Background(), EmrWorkerRequest{TenantID: 7, RecordingID: 42}, &Record{ID: "rec-1"})

	if len(fake.deductions) != 1 {
		t.Fatalf("deductions = %d, want 1", len(fake.deductions))
	}
	items := itemsByUnit(fake.deductions[0].Items)
	if len(items) != 1 || items["record"] != 1 {
		t.Fatalf("items = %#v, want only record:1", fake.deductions[0].Items)
	}
}

// 预检闸门：额度不足时生成入口直接拒绝（402），不调用模型、也不产生扣量。
func TestDraftGenerationEntryRejectsWhenQuotaInsufficient(t *testing.T) {
	fake := &fakeBilling{quotaErr: &billing.InsufficientEntitlementError{
		Unit: "record", Current: 0, Required: 1, Shortfall: 1,
	}}
	handler := newDraftTestHandler(fake, 1800, nil)
	body := `{"tenant_id":7,"recording_id":42,"encounter_id":9,"employee_id":11,` +
		`"business_scope":"doctor","role_code":"doctor","cleaned_transcript":"医生：你好"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/internal/emr/records/generate", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.Generate(response, request)

	if response.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402; body=%s", response.Code, response.Body.String())
	}
	if fake.quotaCalls != 1 || fake.quotaTenant != 7 || fake.quotaProduct != "tingxie" {
		t.Fatalf("quota gate not called as expected: %+v", fake)
	}
	// 时长已知时，预检必须同时检查 record 与 hour。
	items := itemsByUnit(fake.quotaItems)
	if len(items) != 2 || items["record"] != 1 || items["hour"] != 0.5 {
		t.Fatalf("quota items = %#v, want record:1 + hour:0.5", fake.quotaItems)
	}
	if len(fake.deductions) != 0 {
		t.Fatalf("rejected generation must not deduct: %+v", fake.deductions)
	}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details struct {
			Unit      string  `json:"unit"`
			Shortfall float64 `json:"shortfall"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != billing.CodeInsufficientEntitlement {
		t.Fatalf("code = %q", payload.Code)
	}
	if payload.Message != billing.MessageInsufficientEntitlement {
		t.Fatalf("message = %q", payload.Message)
	}
	if payload.Details.Unit != "record" || payload.Details.Shortfall != 1 {
		t.Fatalf("details = %+v", payload.Details)
	}
}

// 计费自身故障时闸门放行（避免计费不可用拖垮病历生成），额度不足才拒绝。
func TestDraftQuotaGateFailsOpenOnBillingFailure(t *testing.T) {
	fake := &fakeBilling{quotaErr: errors.New("billing db down")}
	handler := newDraftTestHandler(fake, 1800, nil)
	if err := handler.ensureDraftQuota(context.Background(), 7, 42); err != nil {
		t.Fatalf("billing failure must fail open, got %v", err)
	}
	if fake.quotaCalls != 1 {
		t.Fatalf("quota calls = %d", fake.quotaCalls)
	}
}

// 完整质控新跑一轮：round 1 轮，幂等键用 report.RunID。
func TestQualityRoundDeductsOneRoundForNewRun(t *testing.T) {
	fake := &fakeBilling{}
	service := &Service{billing: fake}

	service.chargeQualityRound(context.Background(), 7, "rec-1",
		&emrquality.Report{RunID: "run-9"}, "rec-1:123", false)

	if len(fake.deductions) != 1 {
		t.Fatalf("deductions = %d, want 1", len(fake.deductions))
	}
	got := fake.deductions[0]
	if got.TenantID != 7 || got.ProductCode != "tingxie" {
		t.Fatalf("unexpected tenant/product: %+v", got)
	}
	if got.IdempotencyKey != "emr:record:rec-1:quality:run-9" {
		t.Fatalf("idempotency key = %q", got.IdempotencyKey)
	}
	if len(got.Items) != 1 || got.Items[0].Unit != "round" || got.Items[0].Amount != 1 {
		t.Fatalf("items = %#v, want round:1", got.Items)
	}
}

// report 落库前没有 RunID 时，幂等键退化为 recordID:时间戳，且同一轮只用一个 token。
func TestQualityRoundFallbackKeyUsesRecordAndTimestamp(t *testing.T) {
	input := qualityRoundDeductionInput(7, "rec-1", &emrquality.Report{}, "rec-1:999")
	if want := "emr:record:rec-1:quality:rec-1:999"; input.IdempotencyKey != want {
		t.Fatalf("idempotency key = %q, want %q", input.IdempotencyKey, want)
	}
	withoutAnything := qualityRoundDeductionInput(7, "rec-1", nil, "")
	if !strings.HasPrefix(withoutAnything.IdempotencyKey, "emr:record:rec-1:quality:rec-1:") {
		t.Fatalf("fallback key = %q", withoutAnything.IdempotencyKey)
	}
	if withoutAnything.IdempotencyKey == input.IdempotencyKey {
		t.Fatal("fallback key must be unique per round")
	}
}

// 提交时复用已有质控报告（ReusableReport/QualityRunID）不扣 round。
func TestQualityRoundReuseDoesNotDeduct(t *testing.T) {
	fake := &fakeBilling{}
	service := &Service{billing: fake}

	service.chargeQualityRound(context.Background(), 7, "rec-1",
		&emrquality.Report{RunID: "run-9"}, "rec-1:123", true)

	if len(fake.deductions) != 0 {
		t.Fatalf("reused quality report must not deduct: %+v", fake.deductions)
	}
}

// 大模型质检未完成（report 带 FailureReason）不算交付一轮完整质控，不扣 round。
func TestQualityRoundNotDeductedWhenQualityRunFailed(t *testing.T) {
	fake := &fakeBilling{}
	service := &Service{billing: fake}

	service.chargeQualityRound(context.Background(), 7, "rec-1",
		&emrquality.Report{FailureReason: "大模型质检结果不完整，请重试；本次未完成病历。"}, "rec-1:123", false)

	if len(fake.deductions) != 0 {
		t.Fatalf("failed quality run must not deduct: %+v", fake.deductions)
	}
}

// 手动保存（save 级质控）与自动保存（working-draft）都不扣量。
// 用已关闭的连接池让 Store 查询快速失败，从而在不连数据库的前提下走完这两个入口。
func TestManualSaveAndAutoSaveDoNotDeduct(t *testing.T) {
	fake := &fakeBilling{}
	pool, err := pgxpool.New(context.Background(), "postgres://user:pass@127.0.0.1:1/lingce_test")
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	pool.Close()
	service := &Service{store: NewStore(pool), billing: fake}

	if _, saveErr := service.ManualSave(context.Background(), 1, 2, "rec-1", ActionRequest{}); saveErr == nil {
		t.Fatal("expected manual save to fail before touching billing")
	}
	if _, autoErr := service.AutoSave(context.Background(), 1, 2, "rec-1", nil, nil); autoErr == nil {
		t.Fatal("expected auto save to fail before touching billing")
	}
	if len(fake.deductions) != 0 {
		t.Fatalf("manual/auto save must not deduct: %+v", fake.deductions)
	}
	if fake.quotaCalls != 0 {
		t.Fatalf("manual/auto save must not run quota gate: %d", fake.quotaCalls)
	}
}

// 扣量失败（幂等重复、额度不足、DB 错误）不影响已成功的业务：只告警、不再向上抛错。
func TestDeductionFailuresDoNotBreakBusiness(t *testing.T) {
	for _, deductErr := range []error{
		billing.ErrAlreadyDeducted,
		&billing.InsufficientEntitlementError{Unit: "record", Current: 0, Required: 1, Shortfall: 1},
		errors.New("billing db down"),
	} {
		fake := &fakeBilling{deductErr: deductErr}
		handler := newDraftTestHandler(fake, 2700, nil)
		handler.chargeGeneratedDraft(context.Background(), EmrWorkerRequest{TenantID: 7, RecordingID: 42}, &Record{ID: "rec-1"})

		service := &Service{billing: fake}
		service.chargeQualityRound(context.Background(), 7, "rec-1", &emrquality.Report{RunID: "run-9"}, "rec-1:1", false)

		// 两个扣量点都真实尝试过；失败被吞掉，业务调用方可以继续返回成功结果。
		if len(fake.deductions) != 2 {
			t.Fatalf("deduct error %v: deductions = %d, want 2", deductErr, len(fake.deductions))
		}
	}
}

// 未接入计费时（旧部署/单测）扣量与预检都是空操作，不能 panic。
func TestBillingNotConfiguredIsNoop(t *testing.T) {
	handler := &InternalHandler{}
	handler.chargeGeneratedDraft(context.Background(), EmrWorkerRequest{TenantID: 7, RecordingID: 42}, &Record{ID: "rec-1"})
	if err := handler.ensureDraftQuota(context.Background(), 7, 42); err != nil {
		t.Fatalf("quota gate without billing must pass, got %v", err)
	}
	service := &Service{}
	service.chargeQualityRound(context.Background(), 7, "rec-1", &emrquality.Report{RunID: "run-9"}, "rec-1:1", false)
}
