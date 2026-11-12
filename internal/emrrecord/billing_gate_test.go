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
)

// 服务暂停/到期不是「计费故障」：预检必须阻断，不能 fail-open 让客户继续白嫖模型。
func TestDraftQuotaGateBlocksPausedAndExpired(t *testing.T) {
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
			handler := newDraftTestHandler(fake, 1800, nil)
			err := handler.ensureDraftQuota(context.Background(), 7, 42)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ensureDraftQuota = %v, want %v", err, tc.want)
			}
		})
	}
}

// 暂停/到期时 HTTP 响应必须是统一的客户口径，且不能出现「运营」。
func TestDraftQuotaRejectionUsesUnifiedCustomerMessage(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{name: "paused", err: billing.ErrSubscriptionPaused, status: http.StatusConflict, code: billing.CodeSubscriptionPaused, message: billing.MessageSubscriptionPaused},
		{name: "expired", err: billing.ErrSubscriptionExpired, status: http.StatusConflict, code: billing.CodeSubscriptionExpired, message: billing.MessageSubscriptionExpired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newDraftTestHandler(&fakeBilling{}, 0, nil)
			recorder := httptest.NewRecorder()
			handler.writeQuotaExhausted(recorder, EmrWorkerRequest{TenantID: 7, RecordingID: 42}, tc.err)

			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, tc.status, recorder.Body.String())
			}
			var payload struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			if payload.Code != tc.code || payload.Message != tc.message {
				t.Fatalf("code/message = %q/%q, want %q/%q", payload.Code, payload.Message, tc.code, tc.message)
			}
			if strings.Contains(payload.Message, "运营") {
				t.Fatalf("客户口径出现内部称呼: %q", payload.Message)
			}
		})
	}
}

// 增量草稿与正式草稿共用同一个幂等键，避免同一份病历被扣两次。
func TestIncrementalDraftUsesSameDraftIdempotencyKey(t *testing.T) {
	record := &Record{
		ID:               "rec-1",
		SourceReferences: map[string]any{"recording_id": float64(42)},
	}
	if got := recordingIDFromRecord(record); got != 42 {
		t.Fatalf("recordingIDFromRecord = %d, want 42", got)
	}
	fake := &fakeBilling{}
	handler := newDraftTestHandler(fake, 2700, nil)
	handler.chargeGeneratedDraft(context.Background(), EmrWorkerRequest{TenantID: 7, RecordingID: recordingIDFromRecord(record)}, record)

	if len(fake.deductions) != 1 {
		t.Fatalf("deductions = %d, want 1", len(fake.deductions))
	}
	got := fake.deductions[0]
	if got.IdempotencyKey != "emr:record:rec-1:draft" {
		t.Fatalf("idempotency key = %q, want the same key as full draft", got.IdempotencyKey)
	}
	items := itemsByUnit(got.Items)
	if items["record"] != 1 || items["hour"] != 0.75 {
		t.Fatalf("items = %#v, want record:1 + hour:0.75", got.Items)
	}

	if got := recordingIDFromRecord(&Record{SourceReferences: map[string]any{"recording_id": "42"}}); got != 42 {
		t.Fatalf("string recording id = %d, want 42", got)
	}
	if got := recordingIDFromRecord(&Record{}); got != 0 {
		t.Fatalf("missing recording id = %d, want 0", got)
	}
}

// 提交时质检额度不足/暂停/到期：不再阻断保存，而是返回降级状态并跳过质检。
// 计费自身故障仍然 fail-open（照常质检），不能因为计费不可用阻断医生提交。
func TestQualityRoundQuotaDegradesInsteadOfBlocking(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status string
	}{
		{name: "paused", err: billing.ErrSubscriptionPaused, status: QualityStatusSkippedPaused},
		{name: "expired", err: billing.ErrSubscriptionExpired, status: QualityStatusSkippedExpired},
		{
			name:   "insufficient",
			err:    &billing.InsufficientEntitlementError{Unit: "round", Current: 0, Required: 1, Shortfall: 1},
			status: QualityStatusSkippedNoQuota,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeBilling{quotaErr: tc.err}
			svc := &Service{billing: fake}
			degraded := svc.qualityRoundSkip(context.Background(), 7)
			if degraded == nil || degraded.Status != tc.status {
				t.Fatalf("qualityRoundSkip = %#v, want status %q", degraded, tc.status)
			}
			if degraded.Message == "" || degraded.At.IsZero() {
				t.Fatalf("degradation must carry message and time: %#v", degraded)
			}
			if tc.status == QualityStatusSkippedNoQuota && degraded.Message != QualitySkipNoQuotaMessage {
				t.Fatalf("message = %q, want %q", degraded.Message, QualitySkipNoQuotaMessage)
			}
			if len(fake.quotaItems) != 1 || fake.quotaItems[0].Unit != "round" || fake.quotaItems[0].Amount != 1 {
				t.Fatalf("quota items = %#v, want round:1", fake.quotaItems)
			}
			if len(fake.deductions) != 0 {
				t.Fatalf("precheck must not deduct")
			}
		})
	}

	// 计费自身故障不是额度不足：照常质检（返回 nil），不能扩大阻断范围。
	fake := &fakeBilling{quotaErr: errors.New("billing db down")}
	svc := &Service{billing: fake}
	if degraded := svc.qualityRoundSkip(context.Background(), 7); degraded != nil {
		t.Fatalf("billing failure must fail open, got %#v", degraded)
	}

	// 未接入计费（旧部署/单测）：不预检、不降级。
	if degraded := (&Service{}).qualityRoundSkip(context.Background(), 7); degraded != nil {
		t.Fatalf("billing not configured must not degrade, got %#v", degraded)
	}
}
