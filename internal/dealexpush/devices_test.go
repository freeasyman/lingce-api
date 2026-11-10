package dealexpush

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

func TestRegisterDeviceUpsertsIdempotently(t *testing.T) {
	store := newFakeDeviceStore()
	service := newPushTestService(store, &fakeExpoSender{})
	req := RegisterPushDeviceRequest{
		Token:      " ExponentPushToken[abc] ",
		Platform:   "iOS",
		DeviceName: "iPhone 15",
		AppVersion: "1.0.0",
	}

	first, err := service.RegisterDevice(context.Background(), 1, 42, req)
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	second, err := service.RegisterDevice(context.Background(), 1, 42, req)
	if err != nil {
		t.Fatalf("second register: %v", err)
	}

	if len(store.rows) != 1 {
		t.Fatalf("rows = %d, want 1 (idempotent upsert)", len(store.rows))
	}
	if first.ID != second.ID {
		t.Fatalf("ids = %d/%d, want same", first.ID, second.ID)
	}
	if store.upserts != 2 {
		t.Fatalf("upsert calls = %d, want 2", store.upserts)
	}
	saved := store.rows[deviceKey(1, 42, "ExponentPushToken[abc]")]
	if saved.Platform != "ios" {
		t.Fatalf("platform = %q, want ios", saved.Platform)
	}
	if saved.Token != "ExponentPushToken[abc]" {
		t.Fatalf("token = %q", saved.Token)
	}
	if saved.DeviceName != "iPhone 15" {
		t.Fatalf("device name = %q", saved.DeviceName)
	}
}

func TestRegisterDeviceValidation(t *testing.T) {
	service := newPushTestService(newFakeDeviceStore(), &fakeExpoSender{})
	cases := []struct {
		name string
		req  RegisterPushDeviceRequest
		want error
	}{
		{"empty token", RegisterPushDeviceRequest{Token: "", Platform: "ios"}, ErrInvalidPushToken},
		{"blank token", RegisterPushDeviceRequest{Token: "   ", Platform: "ios"}, ErrInvalidPushToken},
		{"token with space", RegisterPushDeviceRequest{Token: "Exponent PushToken[a]", Platform: "ios"}, ErrInvalidPushToken},
		{"invalid platform", RegisterPushDeviceRequest{Token: "ExponentPushToken[a]", Platform: "web"}, ErrInvalidPushPlatform},
		{"too long token", RegisterPushDeviceRequest{Token: strings.Repeat("a", 256), Platform: "ios"}, ErrInvalidPushToken},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if _, err := service.RegisterDevice(context.Background(), 1, 42, item.req); !errors.Is(err, item.want) {
				t.Fatalf("err = %v, want %v", err, item.want)
			}
		})
	}
}

func TestUnregisterDevice(t *testing.T) {
	store := newFakeDeviceStore()
	service := newPushTestService(store, &fakeExpoSender{})
	if _, err := service.RegisterDevice(context.Background(), 1, 42, RegisterPushDeviceRequest{
		Token: "ExponentPushToken[abc]", Platform: "android",
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := service.UnregisterDevice(context.Background(), 1, 42, " ExponentPushToken[abc] "); err != nil {
		t.Fatalf("unregister: %v", err)
	}
	if len(store.deactivated) != 1 || store.deactivated[0] != "ExponentPushToken[abc]" {
		t.Fatalf("deactivated = %v", store.deactivated)
	}
	if store.rows[deviceKey(1, 42, "ExponentPushToken[abc]")].IsActive {
		t.Fatal("device must be inactive after unregister")
	}
}

func TestDeviceIDForTokenFitsColumn(t *testing.T) {
	longToken := strings.Repeat("x", 200)
	deviceID := deviceIDForToken(longToken)
	if len([]rune(deviceID)) > 120 {
		t.Fatalf("device id length = %d, want <= 120", len([]rune(deviceID)))
	}
	if deviceID != deviceIDForToken(longToken) {
		t.Fatal("device id must be stable")
	}
	if got := deviceIDForToken("short-token"); got != "short-token" {
		t.Fatalf("short device id = %q", got)
	}
}

func TestPushDeviceRoutesRegisterAndUnregister(t *testing.T) {
	store := newFakeDeviceStore()
	service := newPushTestService(store, &fakeExpoSender{})
	handler := NewHandler(service)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "test-secret")
	tenantID := int64(1)
	token, _, err := auth.GenerateToken("test-secret", 42, auth.UserTypeMobile, &tenantID, 1, 1)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	body := `{"token":"ExponentPushToken[abc]","platform":"ios","device_name":"iPhone"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deal-expert/push-devices", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d body = %s", rec.Code, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if response["ok"] != true {
		t.Fatalf("register response = %v", response)
	}
	if len(store.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(store.rows))
	}

	// 重复注册不重复。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/deal-expert/push-devices", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("repeat register status = %d", rec.Code)
	}
	if len(store.rows) != 1 {
		t.Fatalf("rows after repeat = %d, want 1", len(store.rows))
	}

	// 注销。
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/deal-expert/push-devices/"+url.PathEscape("ExponentPushToken[abc]"), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unregister status = %d body = %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode unregister response: %v", err)
	}
	if response["ok"] != true {
		t.Fatalf("unregister response = %v", response)
	}
	if len(store.deactivated) != 1 {
		t.Fatalf("deactivated = %v", store.deactivated)
	}
}

func TestPushDeviceRouteRejectsMissingToken(t *testing.T) {
	service := newPushTestService(newFakeDeviceStore(), &fakeExpoSender{})
	handler := NewHandler(service)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, "test-secret")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/deal-expert/push-devices", strings.NewReader(`{"token":"x","platform":"ios"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
