package patientops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
)

// fakeBilling 是 billingService 的内存实现：记录每次扣量与预检的入参，
// 让扣量口径（unit/amount/idempotency key）可以直接断言，不依赖数据库。
//
// 它按幂等键模拟计费层语义：同一 key 第二次调用返回 ErrAlreadyDeducted。
type fakeBilling struct {
	mu sync.Mutex

	deductions []billing.DeductInput
	quotaCalls []fakeQuotaCall

	quotaErr     error
	insufficient bool
	deductErr    error

	seen    map[string]int
	applied map[string]int
}

type fakeQuotaCall struct {
	tenantID    int64
	productCode string
	items       []billing.DeductItem
}

func (f *fakeBilling) Deduct(_ context.Context, input billing.DeductInput) (*billing.DeductResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deductions = append(f.deductions, input)
	if f.deductErr != nil {
		return nil, f.deductErr
	}
	if f.seen == nil {
		f.seen = map[string]int{}
		f.applied = map[string]int{}
	}
	f.seen[input.IdempotencyKey]++
	if f.seen[input.IdempotencyKey] > 1 {
		return nil, billing.ErrAlreadyDeducted
	}
	f.applied[input.IdempotencyKey]++
	return &billing.DeductResult{}, nil
}

func (f *fakeBilling) CheckQuota(_ context.Context, tenantID int64, productCode string, items []billing.DeductItem) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quotaCalls = append(f.quotaCalls, fakeQuotaCall{
		tenantID:    tenantID,
		productCode: productCode,
		items:       append([]billing.DeductItem(nil), items...),
	})
	if f.quotaErr != nil {
		return f.quotaErr
	}
	if f.insufficient && len(items) > 0 {
		return &billing.InsufficientEntitlementError{
			Unit:      items[0].Unit,
			Current:   0,
			Required:  items[0].Amount,
			Shortfall: items[0].Amount,
		}
	}
	return nil
}

func (f *fakeBilling) deductionCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deductions)
}

// newBillingTestService 构造一个只接计费、不接数据库的 Service：
// 录音时长走注入的 lookupRecordingDuration，固定 45 分钟 = 0.75 小时。
func newBillingTestService(fake *fakeBilling) *Service {
	return &Service{
		billing: fake,
		lookupRecordingDuration: func(context.Context, int64, int64) (int64, error) {
			return 2700, nil
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

// 交付点一：录音分析成功后 hour = 录音时长（秒/3600，保留 2 位）。
func TestRecordingAnalysisDeductsRecordingHours(t *testing.T) {
	fake := &fakeBilling{}
	svc := newBillingTestService(fake)

	svc.chargeRecordingAnalysis(context.Background(), 7, 42)

	if got := fake.deductionCount(); got != 1 {
		t.Fatalf("deductions = %d, want 1", got)
	}
	input := fake.deductions[0]
	if input.TenantID != 7 || input.ProductCode != "kehu" {
		t.Fatalf("unexpected tenant/product: %+v", input)
	}
	if input.IdempotencyKey != "patientops:recording:42:analysis" {
		t.Fatalf("idempotency key = %q", input.IdempotencyKey)
	}
	if input.Reason == "" {
		t.Fatal("reason is required")
	}
	if items := itemsByUnit(input.Items); len(items) != 1 || items["hour"] != 0.75 {
		t.Fatalf("items = %#v, want only hour:0.75", input.Items)
	}
}

// 录音时长未知（查询失败或为 0）时跳过扣量，不产生 0 金额计量项。
func TestRecordingAnalysisSkipsUnknownDuration(t *testing.T) {
	cases := []struct {
		name   string
		lookup func(context.Context, int64, int64) (int64, error)
	}{
		{name: "query failed", lookup: func(context.Context, int64, int64) (int64, error) {
			return 0, errors.New("recording missing")
		}},
		{name: "duration zero", lookup: func(context.Context, int64, int64) (int64, error) {
			return 0, nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeBilling{}
			svc := &Service{billing: fake, lookupRecordingDuration: tc.lookup}
			svc.chargeRecordingAnalysis(context.Background(), 7, 42)
			if got := fake.deductionCount(); got != 0 {
				t.Fatalf("deductions = %d, want 0", got)
			}
		})
	}
}

// 交付点二：截图解读成功后 shot 1 张。
func TestScreenshotReviewDeductsOneShot(t *testing.T) {
	fake := &fakeBilling{}
	svc := newBillingTestService(fake)

	svc.chargeScreenshotReview(context.Background(), 7, 55)

	if got := fake.deductionCount(); got != 1 {
		t.Fatalf("deductions = %d, want 1", got)
	}
	input := fake.deductions[0]
	if input.TenantID != 7 || input.ProductCode != "kehu" {
		t.Fatalf("unexpected tenant/product: %+v", input)
	}
	if input.IdempotencyKey != "patientops:screenshot:55:review" {
		t.Fatalf("idempotency key = %q", input.IdempotencyKey)
	}
	if items := itemsByUnit(input.Items); len(items) != 1 || items["shot"] != 1 {
		t.Fatalf("items = %#v, want only shot:1", input.Items)
	}
}

// 交付点三：建议动作生成成功后 record 1 条（幂等键带动作名）。
func TestScreenshotActionDeductsOneRecord(t *testing.T) {
	fake := &fakeBilling{}
	svc := newBillingTestService(fake)

	svc.chargeScreenshotAction(context.Background(), 7, 55, "帮我破冰")

	if got := fake.deductionCount(); got != 1 {
		t.Fatalf("deductions = %d, want 1", got)
	}
	input := fake.deductions[0]
	if input.IdempotencyKey != "patientops:screenshot:55:action:帮我破冰" {
		t.Fatalf("idempotency key = %q", input.IdempotencyKey)
	}
	if items := itemsByUnit(input.Items); len(items) != 1 || items["record"] != 1 {
		t.Fatalf("items = %#v, want only record:1", input.Items)
	}
}

// 交付点四：经营方案内容生成成功后每会员 person 1（幂等键用计划成员 ID）。
func TestPlanMemberContentDeductsOnePerson(t *testing.T) {
	fake := &fakeBilling{}
	svc := newBillingTestService(fake)

	svc.chargePlanMembersContent(context.Background(), 7, 9, []int64{77, 78})

	if got := fake.deductionCount(); got != 2 {
		t.Fatalf("deductions = %d, want 2", got)
	}
	keys := []string{
		"patientops:plan:9:member:77:content",
		"patientops:plan:9:member:78:content",
	}
	for i, want := range keys {
		if fake.deductions[i].IdempotencyKey != want {
			t.Fatalf("key[%d] = %q, want %q", i, fake.deductions[i].IdempotencyKey, want)
		}
		if items := itemsByUnit(fake.deductions[i].Items); len(items) != 1 || items["person"] != 1 {
			t.Fatalf("items[%d] = %#v, want only person:1", i, fake.deductions[i].Items)
		}
	}
}

// 同一份交付重复发生（重试/重复点击）时，四个交付点的幂等键保持不变，
// 计费层第二次返回 ErrAlreadyDeducted，视为成功且不重复扣。
func TestRepeatedDeliveryIsIdempotent(t *testing.T) {
	fake := &fakeBilling{}
	svc := newBillingTestService(fake)
	ctx := context.Background()

	deliveries := []struct {
		name  string
		key   string
		again func()
	}{
		{
			name: "recording analysis",
			key:  "patientops:recording:42:analysis",
			again: func() {
				svc.chargeRecordingAnalysis(ctx, 7, 42)
			},
		},
		{
			name: "screenshot review",
			key:  "patientops:screenshot:55:review",
			again: func() {
				svc.chargeScreenshotReview(ctx, 7, 55)
			},
		},
		{
			name: "screenshot action",
			key:  "patientops:screenshot:55:action:帮我破冰",
			again: func() {
				svc.chargeScreenshotAction(ctx, 7, 55, "帮我破冰")
			},
		},
		{
			name: "plan member content",
			key:  "patientops:plan:9:member:77:content",
			again: func() {
				svc.chargePlanMembersContent(ctx, 7, 9, []int64{77})
			},
		},
	}

	for _, delivery := range deliveries {
		delivery.again()
		delivery.again()
	}

	if got := fake.deductionCount(); got != len(deliveries)*2 {
		t.Fatalf("deduct calls = %d, want %d (repeat must be attempted, then deduped)", got, len(deliveries)*2)
	}
	for _, delivery := range deliveries {
		if got := fake.applied[delivery.key]; got != 1 {
			t.Fatalf("%s applied %d times, want 1", delivery.name, got)
		}
		if got := fake.seen[delivery.key]; got != 2 {
			t.Fatalf("%s attempted %d times, want 2", delivery.name, got)
		}
	}
}

// 预检不足时三个用户主动触发的入口都在生成前拒绝，且不产生任何扣量。
func TestQuotaGateRejectsThreeEntries(t *testing.T) {
	fake := &fakeBilling{insufficient: true}
	svc := &Service{billing: fake}
	ctx := context.Background()

	// 1) 截图解读入口：CheckQuota(shot:1)。截图入口只在真正要跑模型时预检，
	// 直接调 AnalyzeScreenshot 需要数据库，这里直接校验同一个 ensureQuota 口径。
	if err := svc.ensureQuota(ctx, 7, billingUnitShot, 1); !errors.Is(err, ErrPatientOpsQuotaExhausted) {
		t.Fatalf("screenshot quota error = %v, want ErrPatientOpsQuotaExhausted", err)
	} else if !strings.Contains(err.Error(), billing.MessageInsufficientEntitlement) {
		t.Fatalf("screenshot quota message = %q", err.Error())
	}

	// 2) 建议动作生成入口：CheckQuota(record:1)
	if _, err := svc.GenerateAction(ctx, 7, 1, 0, GenerateActionRequest{
		SourceID: 55, Action: "帮我破冰",
	}); !errors.Is(err, ErrPatientOpsQuotaExhausted) {
		t.Fatalf("action entry error = %v, want ErrPatientOpsQuotaExhausted", err)
	}

	// 3) 经营方案内容生成入口（发布 / 加员）：CheckQuota(person:1)
	if _, err := svc.PublishPlan(ctx, 7, 1, 9, PublishPlanRequest{}); !errors.Is(err, ErrPatientOpsQuotaExhausted) {
		t.Fatalf("publish plan entry error = %v, want ErrPatientOpsQuotaExhausted", err)
	}
	if _, err := svc.AddPlanMembers(ctx, 7, 1, 9, AddPlanMembersRequest{}); !errors.Is(err, ErrPatientOpsQuotaExhausted) {
		t.Fatalf("add plan members entry error = %v, want ErrPatientOpsQuotaExhausted", err)
	}

	if got := fake.deductionCount(); got != 0 {
		t.Fatalf("deductions = %d, want 0 when quota is exhausted", got)
	}

	wantUnits := []string{"shot", "record", "person", "person"}
	if len(fake.quotaCalls) != len(wantUnits) {
		t.Fatalf("quota checks = %d, want %d", len(fake.quotaCalls), len(wantUnits))
	}
	for i, wantUnit := range wantUnits {
		call := fake.quotaCalls[i]
		if call.tenantID != 7 || call.productCode != "kehu" {
			t.Fatalf("quota call[%d] tenant/product = %d/%q", i, call.tenantID, call.productCode)
		}
		if len(call.items) != 1 || call.items[0].Unit != wantUnit || call.items[0].Amount != 1 {
			t.Fatalf("quota call[%d] items = %#v, want %s:1", i, call.items, wantUnit)
		}
	}
}

// 预检自身报错（计费故障）或计费未接入时一律放行：入口继续走到业务依赖检查，
// 不返回额度不足，也不产生扣量。
func TestQuotaGateFailsOpenOnPrecheckError(t *testing.T) {
	fake := &fakeBilling{quotaErr: errors.New("billing database unavailable")}
	svc := &Service{billing: fake}
	ctx := context.Background()

	_, err := svc.AnalyzeScreenshot(ctx, ScreenshotAnalyzeRequest{
		TenantID: 7, EmployeeID: 1, ImageData: "data:image/png;base64,AAAA",
	})
	if err == nil || errors.Is(err, ErrPatientOpsQuotaExhausted) {
		t.Fatalf("screenshot entry error = %v, want fail-open continuation", err)
	}

	_, err = svc.GenerateAction(ctx, 7, 1, 0, GenerateActionRequest{SourceID: 55, Action: "帮我破冰"})
	if err == nil || errors.Is(err, ErrPatientOpsQuotaExhausted) {
		t.Fatalf("action entry error = %v, want fail-open continuation", err)
	}

	if err := svc.ensurePlanContentQuota(ctx, 7); err != nil {
		t.Fatalf("plan gate error = %v, want nil (fail-open)", err)
	}
	if got := fake.deductionCount(); got != 0 {
		t.Fatalf("deductions = %d, want 0", got)
	}

	// 计费未接入（nil）时同样放行。
	if err := (&Service{}).ensureQuota(ctx, 7, billingUnitShot, 1); err != nil {
		t.Fatalf("gate without billing integration = %v, want nil", err)
	}
}

// 扣量失败只告警：charge* 不返回错误、不 panic，业务结果保持原样（MVP 允许欠扣）。
func TestDeductionFailureDoesNotAffectBusiness(t *testing.T) {
	fake := &fakeBilling{deductErr: errors.New("billing database unavailable")}
	svc := newBillingTestService(fake)
	ctx := context.Background()

	svc.chargeRecordingAnalysis(ctx, 7, 42)
	svc.chargeScreenshotReview(ctx, 7, 55)
	svc.chargeScreenshotAction(ctx, 7, 55, "帮我破冰")
	svc.chargePlanMembersContent(ctx, 7, 9, []int64{77, 78})

	if got := fake.deductionCount(); got != 5 {
		t.Fatalf("deduct attempts = %d, want 5 (deduction is attempted, business is kept)", got)
	}
}

// 闸门拒绝的 HTTP 输出：402 + 模块既有错误格式 + 固定文案。
func TestQuotaExhaustedResponseFormat(t *testing.T) {
	recorder := httptest.NewRecorder()
	insufficient := &billing.InsufficientEntitlementError{Unit: "shot", Current: 0, Required: 1, Shortfall: 1}
	writeQuotaExhausted(recorder, &quotaExhaustedError{insufficient: insufficient})

	if recorder.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402", recorder.Code)
	}
	var body struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Code != billing.CodeInsufficientEntitlement {
		t.Fatalf("code = %q", body.Code)
	}
	if body.Message != billing.MessageInsufficientEntitlement {
		t.Fatalf("message = %q", body.Message)
	}
	if body.Details["product_code"] != "kehu" || body.Details["unit"] != "shot" {
		t.Fatalf("details = %#v", body.Details)
	}
}

// 只有大模型真的生成了 content 的会员才标记 ContentGenerated，
// 发布/加员后据此扣 person；回退模板的会员不扣。
func TestApplyPlanMemberContentMarksOnlyGeneratedMembers(t *testing.T) {
	candidates := []PlanCandidate{
		{CustomerID: 1, Salutation: "张先生"},
		{CustomerID: 2, Salutation: "李女士"},
		{CustomerID: 3},
	}
	index := map[int64]int{1: 0, 2: 1, 3: 2}
	var parsed planMemberContentOutput
	if err := json.Unmarshal([]byte(`{"members":[
		{"customer_id":1,"goal":"推动到院","content":"到院复查"},
		{"customer_id":2,"goal":"推动复购"},
		{"customer_id":99,"content":"无关会员"}
	]}`), &parsed); err != nil {
		t.Fatalf("decode parsed output: %v", err)
	}

	applyPlanMemberContentOutput(candidates, index, parsed)

	if !candidates[0].ContentGenerated {
		t.Fatal("member with generated content should be charged")
	}
	if candidates[0].Content != "张先生，到院复查" {
		t.Fatalf("content = %q, want salutation prefix", candidates[0].Content)
	}
	if candidates[1].ContentGenerated {
		t.Fatal("member without content (template fallback) must not be charged")
	}
	if candidates[2].ContentGenerated {
		t.Fatal("member absent from LLM output must not be charged")
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
