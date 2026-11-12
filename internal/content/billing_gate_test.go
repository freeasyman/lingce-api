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
)

// 服务暂停/到期必须阻断生成入口，不能 fail-open。
func TestEnsureContentQuotaBlocksPausedAndExpired(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "paused", err: billing.ErrSubscriptionPaused, want: billing.ErrSubscriptionPaused},
		{name: "expired", err: billing.ErrSubscriptionExpired, want: billing.ErrSubscriptionExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeContentBilling()
			fake.quotaErr = tc.err
			svc := newContentTestService(fake, 0, nil)

			err := svc.ensureContentQuota(context.Background(), 7, contentBillingUnitRecord, 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ensureContentQuota = %v, want %v", err, tc.want)
			}
		})
	}
}

// 生成入口的三种拒绝统一成客户口径；文案不能出现「运营」。
func TestWriteContentErrorUsesCustomerGate(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name: "expired", err: billing.ErrSubscriptionExpired,
			status: http.StatusConflict, code: billing.CodeSubscriptionExpired, message: billing.MessageSubscriptionExpired,
		},
		{
			name: "paused", err: billing.ErrSubscriptionPaused,
			status: http.StatusConflict, code: billing.CodeSubscriptionPaused, message: billing.MessageSubscriptionPaused,
		},
		{
			name:   "insufficient",
			err:    &contentQuotaExhaustedError{insufficient: &billing.InsufficientEntitlementError{Unit: "shot", Current: 0, Required: 1, Shortfall: 1}},
			status: http.StatusPaymentRequired, code: billing.CodeInsufficientEntitlement, message: billing.MessageInsufficientEntitlement,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeContentError(recorder, tc.err)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, tc.status, recorder.Body.String())
			}
			var body struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if body.Code != tc.code || body.Message != tc.message {
				t.Fatalf("code/message = %q/%q, want %q/%q", body.Code, body.Message, tc.code, tc.message)
			}
			if strings.Contains(body.Message, "运营") {
				t.Fatalf("客户口径出现内部称呼: %q", body.Message)
			}
		})
	}
}

// 同步生图接口过去直接调 GenerateCardImage 返回 URL，既没有预检也没有扣量。
// 现在必须按实际交付张数扣 shot，且同一 Idempotency-Key 只扣一次。
func TestCardImageBilling(t *testing.T) {
	if got := normalizeCardImageCount(0); got != 3 {
		t.Fatalf("default count = %d, want 3", got)
	}
	if got := normalizeCardImageCount(9); got != 4 {
		t.Fatalf("max count = %d, want 4", got)
	}
	if got := normalizeCardImageCount(2); got != 2 {
		t.Fatalf("count = %d, want 2", got)
	}

	input, ok := cardImageDeductionInput(7, 2, "content:card-image:req-1")
	if !ok {
		t.Fatal("expected card image deduction input")
	}
	if input.ProductCode != contentBillingProductCode || len(input.Items) != 1 ||
		input.Items[0].Unit != contentBillingUnitShot || input.Items[0].Amount != 2 {
		t.Fatalf("unexpected input: %#v", input)
	}
	if input.IdempotencyKey != "content:card-image:req-1" {
		t.Fatalf("idempotency key = %q", input.IdempotencyKey)
	}
	if _, ok := cardImageDeductionInput(7, 0, "k"); ok {
		t.Fatal("zero delivered images must not deduct")
	}
	if _, ok := cardImageDeductionInput(7, 1, " "); ok {
		t.Fatal("empty idempotency key must not deduct")
	}

	fake := newFakeContentBilling()
	svc := newContentTestService(fake, 0, nil)
	svc.chargeCardImages(context.Background(), 7, 2, "content:card-image:req-1")
	svc.chargeCardImages(context.Background(), 7, 2, "content:card-image:req-1")
	if got := fake.chargedCount("content:card-image:req-1"); got != 1 {
		t.Fatalf("card image charged %d times, want 1", got)
	}
}

// 空交付判定：0 条洞察 / 全部引擎失败 / 空正文都不能扣量。
func TestContentEmptyDeliveryGuards(t *testing.T) {
	if insightDeliveryComplete(nil) {
		t.Fatal("0 insights must not be treated as a delivery")
	}
	if !insightDeliveryComplete([]Insight{{Topic: "种植牙"}}) {
		t.Fatal("non-empty insights should be deliverable")
	}

	if aiVisibilityDeliveryComplete(nil) {
		t.Fatal("no engine results must not be treated as a delivery")
	}
	if aiVisibilityDeliveryComplete([]aiEngineAnswer{{Engine: "a", Error: "timeout"}}) {
		t.Fatal("all-failed engines must not be treated as a delivery")
	}
	if !aiVisibilityDeliveryComplete([]aiEngineAnswer{{Engine: "a", Answer: "回答"}}) {
		t.Fatal("one usable answer should be deliverable")
	}
	if !aiVisibilityDeliveryComplete([]aiEngineAnswer{{Engine: "a", Analysis: map[string]any{"mentioned": true}}}) {
		t.Fatal("one usable analysis should be deliverable")
	}

	if aiVisibilityArticleDeliverable("   ") {
		t.Fatal("blank article body must not be treated as a delivery")
	}
	if !aiVisibilityArticleDeliverable("正文") {
		t.Fatal("non-empty article body should be deliverable")
	}
}

// 同步生图的 Idempotency-Key 必须规范化成计费层可接受的格式，否则扣量会被
// INVALID_REQUEST 拒掉、图片却已经交付（漏扣）。
func TestCardImageIdempotencyKey(t *testing.T) {
	if got := cardImageIdempotencyKey("req-123"); got != "content:card-image:req-123" {
		t.Fatalf("key = %q", got)
	}
	if got := cardImageIdempotencyKey("  req-456  "); got != "content:card-image:req-456" {
		t.Fatalf("trimmed key = %q", got)
	}
	// 缺失/含空白/非法空段/超长都退回唯一键（仍可扣量）。
	for _, header := range []string{"", "has space", "a::b", strings.Repeat("x", 200)} {
		got := cardImageIdempotencyKey(header)
		if !strings.HasPrefix(got, "content:card-image:") || len(got) > 128 {
			t.Fatalf("header %q -> invalid key %q", header, got)
		}
		if got == cardImageIdempotencyKey(header) && header != "" {
			t.Fatalf("unexpected deterministic key for invalid header %q", header)
		}
	}
}
