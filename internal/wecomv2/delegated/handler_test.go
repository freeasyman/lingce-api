package delegated

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestIsLicenseUnavailableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "48002", err: errors.New("wecom api error: 48002 api forbidden"), want: true},
		{name: "structured 701000", err: &wecomAPIError{Code: 701000, Msg: "license unavailable"}, want: true},
		{name: "structured unrelated", err: &wecomAPIError{Code: 40029, Msg: "invalid code"}, want: false},
		{name: "license text", err: errors.New("接口调用许可未开通"), want: true},
		{name: "unrelated", err: errors.New("wecom api error: 40029 invalid code"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isLicenseUnavailableError(tt.err); got != tt.want {
				t.Fatalf("isLicenseUnavailableError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsReadyLicenseStatus(t *testing.T) {
	for status, want := range map[int]bool{0: false, 1: true, 2: true, 3: false, 99: false} {
		if got := isReadyLicenseStatus(status); got != want {
			t.Fatalf("isReadyLicenseStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestSetLicenseAutoActiveStatusRequest(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/cgi-bin/license/set_auto_active_status" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("provider_access_token") != "token+/=" {
			t.Fatalf("provider token was not query escaped: %q", r.URL.Query().Get("provider_access_token"))
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer server.Close()

	c := newClient(server.URL, "suite", "secret", "", "")
	if err := c.SetLicenseAutoActiveStatus(context.Background(), "token+/=", "corp"); err != nil {
		t.Fatalf("SetLicenseAutoActiveStatus() error = %v", err)
	}
	if got["auth_corpid"] != "corp" || got["suite_id"] != "suite" || got["auto_active"] != float64(1) {
		t.Fatalf("unexpected request payload: %#v", got)
	}
}

func TestSetLicenseAutoActiveStatusEscapesToken(t *testing.T) {
	if _, err := url.Parse("https://example.test/cgi-bin/license/set_auto_active_status?provider_access_token=" + url.QueryEscape("token+/=")); err != nil {
		t.Fatal(err)
	}
}
