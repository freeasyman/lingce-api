package dealexpush

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeDeviceStore 用内存 map 模拟 mobile_push_devices，验证注册幂等与发送后状态。
type fakeDeviceStore struct {
	rows        map[string]*PushDevice
	nextID      int64
	upserts     int
	deactivated []string
	sent        []string
	failed      map[string]string
	invalid     map[string]string
}

func newFakeDeviceStore() *fakeDeviceStore {
	return &fakeDeviceStore{
		rows:    make(map[string]*PushDevice),
		failed:  make(map[string]string),
		invalid: make(map[string]string),
	}
}

func deviceKey(tenantID, employeeID int64, token string) string {
	return fmt.Sprintf("%d:%d:%s", tenantID, employeeID, token)
}

func (f *fakeDeviceStore) Upsert(_ context.Context, device PushDevice) (*PushDevice, error) {
	f.upserts++
	key := deviceKey(device.TenantID, device.EmployeeID, device.Token)
	if existing, ok := f.rows[key]; ok {
		existing.Platform = device.Platform
		existing.DeviceID = device.DeviceID
		existing.DeviceName = device.DeviceName
		existing.AppVersion = device.AppVersion
		existing.IsActive = true
		return existing, nil
	}
	f.nextID++
	copied := device
	copied.ID = f.nextID
	copied.IsActive = true
	f.rows[key] = &copied
	return &copied, nil
}

func (f *fakeDeviceStore) Deactivate(_ context.Context, tenantID, employeeID int64, token string) error {
	f.deactivated = append(f.deactivated, token)
	if row, ok := f.rows[deviceKey(tenantID, employeeID, token)]; ok {
		row.IsActive = false
	}
	return nil
}

func (f *fakeDeviceStore) ActiveTokens(_ context.Context, tenantID, employeeID int64) ([]PushDevice, error) {
	out := make([]PushDevice, 0)
	for _, row := range f.rows {
		if row.TenantID == tenantID && row.EmployeeID == employeeID && row.IsActive && row.Token != "" {
			out = append(out, *row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (f *fakeDeviceStore) MarkSent(_ context.Context, tenantID, employeeID int64, token string, _ time.Time) error {
	f.sent = append(f.sent, token)
	if row, ok := f.rows[deviceKey(tenantID, employeeID, token)]; ok {
		row.LastError = ""
	}
	return nil
}

func (f *fakeDeviceStore) MarkFailed(_ context.Context, tenantID, employeeID int64, token, reason string) error {
	f.failed[token] = reason
	return nil
}

func (f *fakeDeviceStore) MarkInvalid(_ context.Context, tenantID, employeeID int64, token, reason string) error {
	f.invalid[token] = reason
	if row, ok := f.rows[deviceKey(tenantID, employeeID, token)]; ok {
		row.IsActive = false
		row.LastError = reason
	}
	return nil
}

// fakeExpoSender 记录调用并返回预设回执。
type fakeExpoSender struct {
	calls    int
	messages []ExpoMessage
	tickets  []ExpoTicket
	err      error
}

func (f *fakeExpoSender) Send(_ context.Context, messages []ExpoMessage) ([]ExpoTicket, error) {
	f.calls++
	f.messages = append(f.messages, messages...)
	if f.err != nil {
		return nil, f.err
	}
	if f.tickets != nil {
		return f.tickets, nil
	}
	tickets := make([]ExpoTicket, 0, len(messages))
	for index := range messages {
		tickets = append(tickets, ExpoTicket{Status: "ok", ID: fmt.Sprintf("ticket-%d", index)})
	}
	return tickets, nil
}

func newPushTestService(store PushDeviceStore, expo ExpoSender) *Service {
	return &Service{
		cfg:     enabledDryRunConfig(false, true, nil),
		log:     testLogger(),
		devices: store,
		expo:    expo,
	}
}

func addActiveDevice(store *fakeDeviceStore, tenantID, employeeID int64, token string) {
	store.rows[deviceKey(tenantID, employeeID, token)] = &PushDevice{
		ID:         int64(len(store.rows) + 1),
		TenantID:   tenantID,
		EmployeeID: employeeID,
		Platform:   "ios",
		Token:      token,
		IsActive:   true,
	}
}

func TestSendExpoForEventEncounterPayload(t *testing.T) {
	store := newFakeDeviceStore()
	addActiveDevice(store, 1, 42, "ExponentPushToken[consultant]")
	expo := &fakeExpoSender{}
	service := newPushTestService(store, expo)

	service.sendExpoForEvent(context.Background(), expoPushEvent{
		EventID: 9, TenantID: 1, RecordingID: 100,
		RecipientEmployeeID: 42, RecipientRole: "consultant", CustomerName: "杜女士",
	})

	if expo.calls != 1 || len(expo.messages) != 1 {
		t.Fatalf("expo calls=%d messages=%d, want 1/1", expo.calls, len(expo.messages))
	}
	message := expo.messages[0]
	if message.To != "ExponentPushToken[consultant]" {
		t.Fatalf("to = %q", message.To)
	}
	if message.Title != "接待分析已完成" {
		t.Fatalf("title = %q", message.Title)
	}
	if message.Body != "杜女士的接待分析已完成，点击查看" {
		t.Fatalf("body = %q", message.Body)
	}
	if got := message.Data["type"]; got != "encounter" {
		t.Fatalf("data.type = %v, want encounter", got)
	}
	if got := message.Data["recording_id"]; got != int64(100) {
		t.Fatalf("data.recording_id = %v", got)
	}
	if got := message.Data["employee_id"]; got != int64(42) {
		t.Fatalf("data.employee_id = %v", got)
	}
	if len(store.sent) != 1 || store.sent[0] != "ExponentPushToken[consultant]" {
		t.Fatalf("sent = %v", store.sent)
	}
}

func TestSendExpoForEventInterventionPayload(t *testing.T) {
	store := newFakeDeviceStore()
	addActiveDevice(store, 1, 7, "ExponentPushToken[manager]")
	expo := &fakeExpoSender{}
	service := newPushTestService(store, expo)

	service.sendExpoForEvent(context.Background(), expoPushEvent{
		EventID: 10, TenantID: 1, RecordingID: 101,
		RecipientEmployeeID: 7, RecipientRole: "manager", CustomerName: "王先生",
	})

	if len(expo.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(expo.messages))
	}
	message := expo.messages[0]
	if message.Data["type"] != "intervention" {
		t.Fatalf("data.type = %v, want intervention", message.Data["type"])
	}
	if message.Title != "紧要介入提醒" {
		t.Fatalf("title = %q", message.Title)
	}
	if message.Body != "王先生需要您介入处理，点击查看" {
		t.Fatalf("body = %q", message.Body)
	}
}

func TestSendExpoForEventDeviceNotRegistered(t *testing.T) {
	store := newFakeDeviceStore()
	addActiveDevice(store, 1, 42, "ExponentPushToken[dead]")
	expo := &fakeExpoSender{tickets: []ExpoTicket{{
		Status: "error", ErrorCode: "DeviceNotRegistered", Message: "not registered",
	}}}
	service := newPushTestService(store, expo)

	service.sendExpoForEvent(context.Background(), expoPushEvent{
		EventID: 11, TenantID: 1, RecordingID: 102,
		RecipientEmployeeID: 42, RecipientRole: "consultant", CustomerName: "杜女士",
	})

	if got := store.invalid["ExponentPushToken[dead]"]; !strings.Contains(got, "DeviceNotRegistered") {
		t.Fatalf("invalid reason = %q", got)
	}
	if len(store.sent) != 0 {
		t.Fatalf("sent = %v, want none", store.sent)
	}
	if store.rows[deviceKey(1, 42, "ExponentPushToken[dead]")].IsActive {
		t.Fatal("invalid token must be deactivated")
	}
}

func TestSendExpoForEventNoTokenSkipsSilently(t *testing.T) {
	store := newFakeDeviceStore()
	expo := &fakeExpoSender{}
	service := newPushTestService(store, expo)

	service.sendExpoForEvent(context.Background(), expoPushEvent{
		EventID: 12, TenantID: 1, RecordingID: 103,
		RecipientEmployeeID: 42, RecipientRole: "consultant", CustomerName: "杜女士",
	})

	if expo.calls != 0 {
		t.Fatalf("expo calls = %d, want 0", expo.calls)
	}
}

func TestSendExpoForEventTransportFailureMarksTokensFailed(t *testing.T) {
	store := newFakeDeviceStore()
	addActiveDevice(store, 1, 42, "ExponentPushToken[a]")
	expo := &fakeExpoSender{err: errors.New("network down")}
	service := newPushTestService(store, expo)

	service.sendExpoForEvent(context.Background(), expoPushEvent{
		EventID: 13, TenantID: 1, RecordingID: 104,
		RecipientEmployeeID: 42, RecipientRole: "consultant", CustomerName: "杜女士",
	})

	if got := store.failed["ExponentPushToken[a]"]; !strings.Contains(got, "network down") {
		t.Fatalf("failed reason = %q", got)
	}
	if len(store.invalid) != 0 {
		t.Fatalf("transport failure must not invalidate token: %v", store.invalid)
	}
}

func TestSendExpoForEventPartialBatch(t *testing.T) {
	store := newFakeDeviceStore()
	addActiveDevice(store, 1, 42, "ExponentPushToken[ok]")
	addActiveDevice(store, 1, 42, "ExponentPushToken[bad]")
	expo := &fakeExpoSender{tickets: []ExpoTicket{
		{Status: "ok", ID: "t1"},
		{Status: "error", ErrorCode: "MessageRateExceeded", Message: "too many"},
	}}
	service := newPushTestService(store, expo)

	service.sendExpoForEvent(context.Background(), expoPushEvent{
		EventID: 14, TenantID: 1, RecordingID: 105,
		RecipientEmployeeID: 42, RecipientRole: "consultant", CustomerName: "杜女士",
	})

	if len(store.sent) != 1 || store.sent[0] != "ExponentPushToken[ok]" {
		t.Fatalf("sent = %v", store.sent)
	}
	if _, ok := store.failed["ExponentPushToken[bad]"]; !ok {
		t.Fatalf("failed = %v", store.failed)
	}
}

func TestExpoPushCopyAndSalutationDoNotLeakSensitiveFields(t *testing.T) {
	cases := []struct {
		name, gender, want string
	}{
		{"王小明", "男", "王先生"},
		{"杜舒然", "女", "杜女士"},
		{"杜女士", "", "杜女士"},
		{"", "", "客户"},
		{"客户", "男", "客户"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := pushSalutation(item.name, item.gender); got != item.want {
				t.Fatalf("pushSalutation(%q,%q) = %q, want %q", item.name, item.gender, got, item.want)
			}
		})
	}
	if got := pushSalutation("王小明", "男"); strings.Contains(got, "小明") {
		t.Fatalf("push salutation leaked full name: %q", got)
	}

	title, body := expoPushCopy("consultant", "杜女士")
	if strings.ContainsAny(title+body, "0123456789") {
		t.Fatalf("push copy must not contain amounts/numbers: %q %q", title, body)
	}
	for _, forbidden := range []string{"金额", "病情", "诊断", "杜舒然"} {
		if strings.Contains(title+body, forbidden) {
			t.Fatalf("push copy leaked %q: %q %q", forbidden, title, body)
		}
	}
}

func TestExpoPushType(t *testing.T) {
	cases := map[string]string{
		"consultant":   "encounter",
		"manager":      "intervention",
		"intervention": "intervention",
		"":             "encounter",
	}
	for role, want := range cases {
		if got := expoPushType(role); got != want {
			t.Fatalf("expoPushType(%q) = %q, want %q", role, got, want)
		}
	}
}
