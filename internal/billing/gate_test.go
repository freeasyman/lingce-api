package billing

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 服务闸门：暂停优先于到期；两种状态都必须拒绝，不能当成计费故障放行。
func TestSubscriptionGuardMapsPausedAndExpired(t *testing.T) {
	cases := []struct {
		name  string
		state string
		want  error
	}{
		{name: "paused", state: InstitutionStatusPaused, want: ErrSubscriptionPaused},
		{name: "expired", state: InstitutionStatusExpired, want: ErrSubscriptionExpired},
		{name: "paused wins over expired", state: InstitutionStatusPaused, want: ErrSubscriptionPaused},
		{name: "active", state: "", want: nil},
		{name: "trial is readable", state: "", want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := subscriptionGuard(tc.state)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("subscriptionGuard(%q) = %v, want nil", tc.state, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("subscriptionGuard(%q) = %v, want %v", tc.state, err, tc.want)
			}
		})
	}
}

// 三种客户侧拒绝的 (status, code, message) 必须完全固定。
func TestGateRejectionCustomerMessages(t *testing.T) {
	insufficient := &InsufficientEntitlementError{Unit: "hour", Current: 1, Required: 2, Shortfall: 1}
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name: "expired", err: fmt.Errorf("check quota: %w", ErrSubscriptionExpired),
			status: http.StatusConflict, code: CodeSubscriptionExpired, message: MessageSubscriptionExpired,
		},
		{
			name: "paused", err: fmt.Errorf("deduct: %w", ErrSubscriptionPaused),
			status: http.StatusConflict, code: CodeSubscriptionPaused, message: MessageSubscriptionPaused,
		},
		{
			name: "insufficient", err: fmt.Errorf("check quota: %w", insufficient),
			status: http.StatusPaymentRequired, code: CodeInsufficientEntitlement, message: MessageInsufficientEntitlement,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gate, ok := GateRejection(tc.err)
			if !ok {
				t.Fatalf("GateRejection(%v) not recognized", tc.err)
			}
			if gate.Status != tc.status || gate.Code != tc.code || gate.Message != tc.message {
				t.Fatalf("gate = %+v, want %d/%s/%q", gate, tc.status, tc.code, tc.message)
			}
		})
	}

	if _, ok := GateRejection(errors.New("billing database down")); ok {
		t.Fatal("计费自身故障不能被当成客户侧交付拒绝")
	}
	if _, ok := GateRejection(nil); ok {
		t.Fatal("nil 不是交付拒绝")
	}

	gate, _ := GateRejection(insufficient)
	if gate.Insufficient == nil || gate.Insufficient.Unit != "hour" || gate.Insufficient.Shortfall != 1 {
		t.Fatalf("insufficient details lost: %+v", gate.Insufficient)
	}
}

// 客户口径文案不能出现「运营」这类内部称呼；三种拒绝都必须带对客可执行的指引。
func TestCustomerGateMessagesAreCustomerFacing(t *testing.T) {
	messages := map[string]string{
		"expired":      MessageSubscriptionExpired,
		"paused":       MessageSubscriptionPaused,
		"insufficient": MessageInsufficientEntitlement,
	}
	for name, message := range messages {
		if strings.Contains(message, "运营") {
			t.Fatalf("%s 文案含内部称呼: %q", name, message)
		}
		if name == "insufficient" {
			if !strings.Contains(message, "客服") {
				t.Fatalf("额度不足文案应引导联系客服: %q", message)
			}
			continue
		}
		if !strings.Contains(message, "专属顾问") {
			t.Fatalf("%s 文案应引导联系专属顾问: %q", name, message)
		}
	}
}

// 内部扣量/预检接口虽然叫 internal，但错误会透传给客户：三种拒绝必须统一成客户口径。
func TestInternalBillingErrorUsesCustomerGate(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "expired", err: ErrSubscriptionExpired, status: http.StatusConflict, code: CodeSubscriptionExpired, message: MessageSubscriptionExpired},
		{name: "paused", err: ErrSubscriptionPaused, status: http.StatusConflict, code: CodeSubscriptionPaused, message: MessageSubscriptionPaused},
		{
			name:    "insufficient",
			err:     &InsufficientEntitlementError{Unit: "record", Current: 0, Required: 1, Shortfall: 1},
			status:  http.StatusConflict,
			code:    CodeInsufficientEntitlement,
			message: MessageInsufficientEntitlement,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeInternalBillingError(recorder, tc.err)
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
