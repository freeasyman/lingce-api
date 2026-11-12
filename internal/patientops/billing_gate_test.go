package patientops

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

// 服务暂停/到期必须阻断客户总监的生成入口，不能 fail-open。
func TestEnsureQuotaBlocksPausedAndExpired(t *testing.T) {
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
			fake := &fakeBilling{quotaErr: tc.err}
			svc := newBillingTestService(fake)

			err := svc.ensureQuota(context.Background(), 7, billingUnitShot, 1)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ensureQuota = %v, want %v", err, tc.want)
			}
			if fake.deductionCount() != 0 {
				t.Fatalf("precheck must not deduct")
			}
		})
	}
}

// 三种拒绝统一客户口径，且文案不含「运营」。
func TestWriteQuotaExhaustedUsesCustomerGate(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "expired", err: billing.ErrSubscriptionExpired, status: http.StatusConflict, code: billing.CodeSubscriptionExpired, message: billing.MessageSubscriptionExpired},
		{name: "paused", err: billing.ErrSubscriptionPaused, status: http.StatusConflict, code: billing.CodeSubscriptionPaused, message: billing.MessageSubscriptionPaused},
		{
			name:    "insufficient",
			err:     &quotaExhaustedError{insufficient: &billing.InsufficientEntitlementError{Unit: "shot", Current: 0, Required: 1, Shortfall: 1}},
			status:  http.StatusPaymentRequired,
			code:    billing.CodeInsufficientEntitlement,
			message: billing.MessageInsufficientEntitlement,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeQuotaExhausted(recorder, tc.err)
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

// 空分析（{} / 只有身份字段）不算交付：不落库、不扣 hour/shot。
func TestScreenshotAnalysisDeliverableContent(t *testing.T) {
	empty := ScreenshotAnalysis{}
	if empty.hasDeliverableContent() {
		t.Fatal("empty analysis must not be deliverable")
	}
	identityOnly := ScreenshotAnalysis{Platform: "微信", Nickname: "小王"}
	if identityOnly.hasDeliverableContent() {
		t.Fatal("identity-only analysis must not be deliverable")
	}
	cases := []ScreenshotAnalysis{
		{ConversationSummary: "客户询问种植牙价格"},
		{PatientQuestion: "种牙多少钱"},
		{Concern: "担心价格"},
		{NextAction: "明天回访"},
		{SuggestedReply: "您好"},
		{SuggestedActions: StringList{"发送案例"}},
		{EmployeeCommitments: StringList{"明天回访"}},
		{Evidence: StringList{"客户：太贵了"}},
	}
	for i, analysis := range cases {
		if !analysis.hasDeliverableContent() {
			t.Fatalf("case %d should be deliverable: %+v", i, analysis)
		}
	}
}

// 已有成功结果的截图是纯读取：复用不再被额度预检误伤。
func TestScreenshotAnalysisReusable(t *testing.T) {
	if !screenshotAnalysisReusable("succeeded", []byte(`{"conversation_summary":"x"}`)) {
		t.Fatal("succeeded with payload should be reusable")
	}
	notReusable := []struct {
		status  string
		payload []byte
	}{
		{"processing", []byte(`{"conversation_summary":"x"}`)},
		{"pending", nil},
		{"failed", []byte(`{"conversation_summary":"x"}`)},
		{"succeeded", nil},
		{"succeeded", []byte(`{}`)},
		{"succeeded", []byte(`null`)},
	}
	for _, item := range notReusable {
		if screenshotAnalysisReusable(item.status, item.payload) {
			t.Fatalf("status=%s payload=%q should not be reusable", item.status, item.payload)
		}
	}
}
