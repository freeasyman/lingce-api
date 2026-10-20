package recording

import (
	"encoding/json"
	"testing"
)

// 赢客专家依赖四个成交事实字段：接待引用、人工确认结果、成交金额、退款金额。
// 这里验证它们确实出现在录音响应 JSON 中，避免前端只能拿到 null。
func TestToRecordingResponseExposesDealFacts(t *testing.T) {
	encounterID := int64(8001)
	status := "还在跟进"
	converted := 23800.0
	refund := 0.0
	resp := toRecordingResponse(&MedicalRecording{
		ID:                  7001,
		TenantID:            9001,
		EncounterID:         &encounterID,
		ConfirmedDealStatus: &status,
		ConvertedAmount:     &converted,
		RefundAmount:        &refund,
	})

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got, ok := body["encounter_id"]; !ok || got != float64(8001) {
		t.Fatalf("encounter_id = %v (present=%v), want 8001", got, ok)
	}
	if got, ok := body["confirmed_deal_status"]; !ok || got != "还在跟进" {
		t.Fatalf("confirmed_deal_status = %v (present=%v), want 还在跟进", got, ok)
	}
	if got, ok := body["converted_amount"]; !ok || got != 23800.0 {
		t.Fatalf("converted_amount = %v (present=%v), want 23800", got, ok)
	}
	if got, ok := body["refund_amount"]; !ok || got != 0.0 {
		t.Fatalf("refund_amount = %v (present=%v), want 0", got, ok)
	}
}
