package emrrealtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func validFollowupRequest() realtimeFollowupRequest {
	return realtimeFollowupRequest{TenantID: 1, RecordingID: 1699, EncounterID: 26, EmployeeID: 117, DoctorName: "医生", CustomerID: 8880, CustomerName: "患者", RecordedAt: "2026-09-13T10:00:00Z", CleanedTranscript: "最终清洗转写"}
}

func TestRealtimeFollowupDeliveryAcceptance(t *testing.T) {
	r := validFollowupRequest()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" || req.URL.Path != "/api/v1/followup/tasks/generate" || req.Header.Get("X-Internal-Token") != "test-token" {
			t.Error("unexpected route or authentication")
		}
		var got realtimeFollowupRequest
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil || got != r {
			t.Errorf("unexpected payload: %#v %v", got, err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "ACCEPTED", "status": "accepted", "request_id": "followup-test", "tenant_id": r.TenantID, "recording_id": r.RecordingID, "encounter_id": r.EncounterID, "employee_id": r.EmployeeID, "customer_id": r.CustomerID})
	}))
	defer server.Close()
	id, err := sendRealtimeFollowup(context.Background(), server.Client(), server.URL, "test-token", r)
	if err != nil || id != "followup-test" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestRealtimeFollowupDeliveryRejectsUnconfirmedResponse(t *testing.T) {
	for _, body := range []string{`{}`, `{"code":"ACCEPTED","status":"accepted","request_id":"x","recording_id":999}`, `invalid`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		if _, err := sendRealtimeFollowup(context.Background(), server.Client(), server.URL, "token", validFollowupRequest()); err == nil {
			t.Errorf("accepted invalid response: %s", body)
		}
		server.Close()
	}
}

func TestRealtimeFollowupRequiresPatientAndTranscript(t *testing.T) {
	r := validFollowupRequest()
	r.CustomerID = 0
	if r.validate() == nil {
		t.Error("missing customer accepted")
	}
	r = validFollowupRequest()
	r.CleanedTranscript = " "
	if r.validate() == nil {
		t.Error("empty transcript accepted")
	}
}

func TestRealtimeFollowupDoesNotAcceptHTTPFailure(t *testing.T) {
	for _, status := range []int{400, 401, 403, 422, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		if _, err := sendRealtimeFollowup(context.Background(), server.Client(), server.URL, "token", validFollowupRequest()); err == nil {
			t.Errorf("HTTP %d accepted", status)
		}
		server.Close()
	}
}
