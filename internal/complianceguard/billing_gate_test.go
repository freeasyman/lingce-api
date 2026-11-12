package complianceguard

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

// 服务暂停/到期必须阻断合规检查，不能 fail-open。
func TestEnsureCommunicationQuotaBlocksPausedAndExpired(t *testing.T) {
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
			fake := newFakeComplianceBilling()
			fake.quotaErr = tc.err
			svc := newComplianceTestService(fake, 3600, nil)

			err := svc.ensureCommunicationQuota(context.Background(), 7, 42)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ensureCommunicationQuota = %v, want %v", err, tc.want)
			}
			if fake.deductCount() != 0 {
				t.Fatalf("precheck must not deduct")
			}
		})
	}
}

// 三种拒绝统一客户口径，文案不含「运营」。
func TestWriteComplianceQuotaExhaustedUsesCustomerGate(t *testing.T) {
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
			err:     &complianceQuotaExhaustedError{insufficient: &billing.InsufficientEntitlementError{Unit: "hour", Current: 0, Required: 1, Shortfall: 1}},
			status:  http.StatusPaymentRequired,
			code:    billing.CodeInsufficientEntitlement,
			message: billing.MessageInsufficientEntitlement,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeComplianceQuotaExhausted(recorder, tc.err)
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
