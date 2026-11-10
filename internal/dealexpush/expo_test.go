package dealexpush

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExpoClientSendBuildsPayload(t *testing.T) {
	var gotMessages []ExpoMessage
	var gotAuth, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotMessages); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"ticket-1"}]}`))
	}))
	defer server.Close()

	client := &ExpoClient{
		endpoint:    server.URL,
		accessToken: "secret",
		httpClient:  server.Client(),
		log:         testLogger(),
	}
	messages := []ExpoMessage{{
		To:    "ExponentPushToken[abc]",
		Title: "接待分析已完成",
		Body:  "杜女士的接待分析已完成，点击查看",
		Data:  map[string]any{"type": "encounter", "recording_id": int64(100), "employee_id": int64(42)},
		Sound: "default",
	}}
	tickets, err := client.Send(context.Background(), messages)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(tickets) != 1 || tickets[0].Status != "ok" || tickets[0].ID != "ticket-1" {
		t.Fatalf("tickets = %+v", tickets)
	}
	if len(gotMessages) != 1 {
		t.Fatalf("request messages = %d", len(gotMessages))
	}
	if gotMessages[0].To != messages[0].To || gotMessages[0].Title != messages[0].Title ||
		gotMessages[0].Body != messages[0].Body || gotMessages[0].Data["type"] != "encounter" {
		t.Fatalf("request message = %+v", gotMessages[0])
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type = %q", gotContentType)
	}
}

func TestExpoClientOmitsAuthorizationWhenTokenEmpty(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"status":"ok","id":"t1"}]}`))
	}))
	defer server.Close()

	client := &ExpoClient{endpoint: server.URL, accessToken: "", httpClient: server.Client(), log: testLogger()}
	if _, err := client.Send(context.Background(), []ExpoMessage{{To: "ExponentPushToken[a]"}}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("authorization = %q, want empty", gotAuth)
	}
}

func TestExpoClientParsesDeviceNotRegistered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"status":"error","message":"not registered","details":{"error":"DeviceNotRegistered"}}]}`))
	}))
	defer server.Close()

	client := &ExpoClient{endpoint: server.URL, httpClient: server.Client(), log: testLogger()}
	tickets, err := client.Send(context.Background(), []ExpoMessage{{To: "ExponentPushToken[dead]"}})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(tickets) != 1 || tickets[0].ErrorCode != "DeviceNotRegistered" {
		t.Fatalf("tickets = %+v", tickets)
	}
}

func TestExpoClientReturnsErrorOnHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream error"))
	}))
	defer server.Close()

	client := &ExpoClient{endpoint: server.URL, httpClient: server.Client(), log: testLogger()}
	if _, err := client.Send(context.Background(), []ExpoMessage{{To: "ExponentPushToken[a]"}}); err == nil {
		t.Fatal("expected http error")
	}
}

func TestExpoClientFillsMissingTickets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	client := &ExpoClient{endpoint: server.URL, httpClient: server.Client(), log: testLogger()}
	tickets, err := client.Send(context.Background(), []ExpoMessage{{To: "a"}, {To: "b"}})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(tickets) != 2 || !strings.Contains(tickets[0].Message, "missing") {
		t.Fatalf("tickets = %+v", tickets)
	}
}

func TestExpoClientSendsInChunksOf100(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var messages []ExpoMessage
		_ = json.NewDecoder(r.Body).Decode(&messages)
		tickets := make([]map[string]any, 0, len(messages))
		for range messages {
			tickets = append(tickets, map[string]any{"status": "ok", "id": "t"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": tickets})
	}))
	defer server.Close()

	messages := make([]ExpoMessage, 101)
	for index := range messages {
		messages[index] = ExpoMessage{To: "ExponentPushToken[x]"}
	}
	client := &ExpoClient{endpoint: server.URL, httpClient: server.Client(), log: testLogger()}
	tickets, err := client.Send(context.Background(), messages)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if calls != 2 {
		t.Fatalf("http calls = %d, want 2", calls)
	}
	if len(tickets) != 101 {
		t.Fatalf("tickets = %d, want 101", len(tickets))
	}
}
