package badge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareClientControlRecordingSuccess(t *testing.T) {
	var gotToken string
	var gotPath string
	var gotQueryVendor string
	var gotMethod string
	var gotPayload startRecordingPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Internal-Token")
		gotPath = r.URL.Path
		gotQueryVendor = r.URL.Query().Get("vendor_code")
		gotMethod = r.Method

		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	operatorID := int64(123)
	extra := JSONObject{"vendor_code": "dudutalk", "order_no": "ORDER-001"}
	client := NewMiddlewareClient(srv.URL, "token-abc")

	err := client.ControlRecording(context.Background(), "start", "DEV-001", &operatorID, extra)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/v1/devices/DEV-001/recording/start" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotQueryVendor != "dudutalk" {
		t.Fatalf("unexpected vendor_code query: %s", gotQueryVendor)
	}
	if gotToken != "token-abc" {
		t.Fatalf("unexpected token header: %s", gotToken)
	}
	if gotPayload.OrderNo != "ORDER-001" {
		t.Fatalf("unexpected order_no: %s", gotPayload.OrderNo)
	}
}

func TestMiddlewareClientControlRecordingErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"device offline"}`))
	}))
	defer srv.Close()

	client := NewMiddlewareClient(srv.URL, "")
	err := client.ControlRecording(context.Background(), "stop", "DEV-002", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "device offline") {
		t.Fatalf("expected parsed error message, got %v", err)
	}
}

func TestMiddlewareClientControlRecordingStopRequestShape(t *testing.T) {
	var gotPath string
	var gotQueryVendor string
	var gotBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQueryVendor = r.URL.Query().Get("vendor_code")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewMiddlewareClient(srv.URL, "")
	err := client.ControlRecording(context.Background(), "stop", "DEV-009", nil, JSONObject{"vendor_code": "dudutalk"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if gotPath != "/v1/devices/DEV-009/recording/stop" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if gotQueryVendor != "dudutalk" {
		t.Fatalf("unexpected vendor_code query: %s", gotQueryVendor)
	}
	if len(gotBody) != 0 {
		t.Fatalf("expected empty object body, got %+v", gotBody)
	}
}

func TestMiddlewareClientControlRecordingWithoutBaseURL(t *testing.T) {
	client := NewMiddlewareClient("", "")
	err := client.ControlRecording(context.Background(), "start", "DEV-003", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "URL is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}
