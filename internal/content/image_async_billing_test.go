package content

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/internal/router"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

// 多图异步预检口径：按本次实际要生成的图位数（非空 prompts）检查 shot，
// 不再固定 1 张。空白 prompt 不产出图片，也不计入预检。
func TestAsyncImagePromptCount(t *testing.T) {
	cases := []struct {
		name    string
		prompts []string
		want    int
	}{
		{name: "five prompts", prompts: []string{"a", "b", "c", "d", "e"}, want: 5},
		{name: "blank prompts ignored", prompts: []string{"a", " ", "", "b"}, want: 2},
		{name: "all blank", prompts: []string{"", "  "}, want: 0},
		{name: "none", prompts: nil, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := asyncImagePromptCount(tc.prompts); got != tc.want {
				t.Fatalf("asyncImagePromptCount(%#v) = %d, want %d", tc.prompts, got, tc.want)
			}
		})
	}
}

// N 张：预检必须要求 shot N。
func TestEnsureAsyncImageQuotaRequiresImageCount(t *testing.T) {
	fake := newFakeContentBilling()
	service := newContentTestService(fake, 0, nil)

	if err := service.ensureAsyncImageQuota(context.Background(), 7, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(fake.quotaChecks) != 1 {
		t.Fatalf("quota checks = %d, want 1", len(fake.quotaChecks))
	}
	check := fake.quotaChecks[0]
	if check.TenantID != 7 || check.ProductCode != contentBillingProductCode {
		t.Fatalf("unexpected quota check target: %#v", check)
	}
	if len(check.Items) != 1 || check.Items[0].Unit != contentBillingUnitShot || check.Items[0].Amount != 5 {
		t.Fatalf("quota items = %#v, want shot:5", check.Items)
	}
	if fake.deductCount() != 0 {
		t.Fatalf("precheck must not deduct, got %d", fake.deductCount())
	}

	// 没有图要生成时不做无意义的 0 额预检。
	if err := service.ensureAsyncImageQuota(context.Background(), 7, 0); err != nil {
		t.Fatalf("zero images must not precheck: %v", err)
	}
	if len(fake.quotaChecks) != 1 {
		t.Fatalf("zero images added a quota check: %#v", fake.quotaChecks)
	}
}

// 余额 N-1 张时整单拒绝：返回 402 INSUFFICIENT_ENTITLEMENT，不开始生成、不扣量。
func TestEnsureAsyncImageQuotaRejectsWhenBalanceBelowImageCount(t *testing.T) {
	fake := newFakeContentBilling()
	fake.quotaErr = &billing.InsufficientEntitlementError{
		Unit: contentBillingUnitShot, Current: 4, Required: 5, Shortfall: 1,
	}
	service := newContentTestService(fake, 0, nil)

	err := service.ensureAsyncImageQuota(context.Background(), 7, 5)
	if !errors.Is(err, ErrContentQuotaExhausted) {
		t.Fatalf("expected ErrContentQuotaExhausted, got %v", err)
	}
	var insufficient *billing.InsufficientEntitlementError
	if !errors.As(err, &insufficient) || insufficient.Unit != contentBillingUnitShot ||
		insufficient.Current != 4 || insufficient.Required != 5 {
		t.Fatalf("quota error should carry current/required details, got %#v", err)
	}
	if fake.deductCount() != 0 {
		t.Fatalf("rejected request must not deduct, got %d", fake.deductCount())
	}
}

// 异步多图的扣量按"成功写入内容的图位"逐张进行：N 张成功 = N 次 shot 1，
// 失败/空交付的图位不扣；同一图位重复生成命中幂等键，不重复扣。
func TestAsyncMultiImageDeductionChargesDeliveredSlots(t *testing.T) {
	fake := newFakeContentBilling()
	service := newContentTestService(fake, 0, nil)

	// 一次生成 5 张，只有图位 0/1/3 成功落库。
	delivered := []int{0, 1, 3}
	for _, slot := range delivered {
		service.chargeImageSlot(context.Background(), 7, 13, slot)
	}
	if got := fake.deductCount(); got != len(delivered) {
		t.Fatalf("deduct attempts = %d, want %d", got, len(delivered))
	}

	total := 0.0
	for _, input := range fake.deducts {
		if input.ProductCode != contentBillingProductCode || len(input.Items) != 1 ||
			input.Items[0].Unit != contentBillingUnitShot {
			t.Fatalf("unexpected deduction: %#v", input)
		}
		total += input.Items[0].Amount
	}
	if total != float64(len(delivered)) {
		t.Fatalf("total deducted shots = %v, want %d", total, len(delivered))
	}

	// 同一图位重试/重生成不会再扣一次。
	service.chargeImageSlot(context.Background(), 7, 13, 3)
	if got := fake.chargedCount("content:image:13-3:deliver"); got != 1 {
		t.Fatalf("slot 3 charged %d times, want 1", got)
	}
}

func testStoryboardBody() string {
	return `{"chapters":[{"shots":[
		{"cast":"医生","visual":"问诊","action":"询问","dialogue":"哪里不舒服"},
		{"cast":"无人物","visual":"诊室全景"},
		{"cast":"患者","visual":"回答","action":"指腹部","dialogue":"肚子疼"},
		{"cast":"医生","visual":"记录","action":"书写","dialogue":"多久了"}
	]}]}`
}

// 分镜"缺失图位"口径：无人物镜头不占图位；已有图的图位跳过；指定镜号只生成 1 张。
func TestStoryboardPendingImageSlots(t *testing.T) {
	jobs, err := storyboardImageJobs(testStoryboardBody())
	if err != nil {
		t.Fatalf("parse storyboard jobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("image jobs = %d, want 3 (无人物不占位)", len(jobs))
	}
	if jobs[0].ImageIndex != 0 || jobs[0].ShotNo != 1 ||
		jobs[1].ImageIndex != 1 || jobs[1].ShotNo != 3 ||
		jobs[2].ImageIndex != 2 || jobs[2].ShotNo != 4 {
		t.Fatalf("unexpected job mapping: %#v", jobs)
	}

	// 图位 0 已有图，本次只生成 1、2 两张。
	images := []string{"https://img/0.png", ""}
	slots := storyboardPendingImageSlots(jobs, images, -1)
	if len(slots) != 2 || slots[0] != 1 || slots[1] != 2 {
		t.Fatalf("pending slots = %#v, want [1 2]", slots)
	}

	// 指定重生成图位 2：只要求 1 张。
	if slots := storyboardPendingImageSlots(jobs, images, 2); len(slots) != 1 || slots[0] != 2 {
		t.Fatalf("regenerate slots = %#v, want [2]", slots)
	}
	// 指定一个不存在的镜头：没有图要生成。
	if slots := storyboardPendingImageSlots(jobs, images, 9); len(slots) != 0 {
		t.Fatalf("unknown shot_index slots = %#v, want empty", slots)
	}
	// 全部已有图：没有缺失图位。
	if slots := storyboardPendingImageSlots(jobs, []string{"a", "b", "c"}, -1); len(slots) != 0 {
		t.Fatalf("complete images slots = %#v, want empty", slots)
	}
	// 稀疏数组短于图位数：缺的都要生成。
	if slots := storyboardPendingImageSlots(jobs, nil, -1); len(slots) != 3 {
		t.Fatalf("empty images slots = %#v, want 3", slots)
	}
}

// 分镜预检按缺失图位数 N 要求额度，余额 N-1 时整单拒绝。
func TestStoryboardAsyncQuotaUsesMissingSlotCount(t *testing.T) {
	jobs, err := storyboardImageJobs(testStoryboardBody())
	if err != nil {
		t.Fatal(err)
	}
	slots := storyboardPendingImageSlots(jobs, []string{"https://img/0.png"}, -1)
	if len(slots) != 2 {
		t.Fatalf("pending slots = %d, want 2", len(slots))
	}

	fake := newFakeContentBilling()
	fake.quotaErr = &billing.InsufficientEntitlementError{
		Unit: contentBillingUnitShot, Current: 1, Required: 2, Shortfall: 1,
	}
	service := newContentTestService(fake, 0, nil)
	err = service.ensureAsyncImageQuota(context.Background(), 7, len(slots))
	if !errors.Is(err, ErrContentQuotaExhausted) {
		t.Fatalf("expected ErrContentQuotaExhausted, got %v", err)
	}
	if len(fake.quotaChecks) != 1 || fake.quotaChecks[0].Items[0].Amount != 2 {
		t.Fatalf("quota checks = %#v, want shot:2", fake.quotaChecks)
	}
}

// 端到端（handler 层）：5 张的异步生图请求，余额只有 4 张时返回 402，
// 响应为统一客户口径，且不会启动后台生成。
func TestGenerateImagesAsyncReturns402WhenBalanceShort(t *testing.T) {
	fake := newFakeContentBilling()
	fake.quotaErr = &billing.InsufficientEntitlementError{
		Unit: contentBillingUnitShot, Current: 4, Required: 5, Shortfall: 1,
	}
	service := newContentTestService(fake, 0, nil)
	handler := NewHandler(service)
	mux := http.NewServeMux()
	handler.registerDraftRoutes(mux, router.RouteDeps{JWTSecret: "content-test-secret"})

	tenantID := int64(7)
	token, _, err := auth.GenerateToken("content-test-secret", 1, auth.UserTypeAdmin, &tenantID, 0, 1)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	body := `{"content_id":13,"prompts":["p1","p2","p3","p4","p5"]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/content-items/actions/generate-images-async", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402; body=%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if payload.Code != billing.CodeInsufficientEntitlement {
		t.Fatalf("code = %q, want %q", payload.Code, billing.CodeInsufficientEntitlement)
	}
	if len(fake.quotaChecks) != 1 || fake.quotaChecks[0].Items[0].Amount != 5 {
		t.Fatalf("precheck must require 5 shots, got %#v", fake.quotaChecks)
	}
	if fake.deductCount() != 0 {
		t.Fatalf("rejected request must not deduct, got %d", fake.deductCount())
	}
}
