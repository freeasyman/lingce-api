package recording

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

func TestIsMultipartUploadRequest(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "multipart with boundary", contentType: "multipart/form-data; boundary=abc", want: true},
		{name: "multipart uppercase", contentType: "Multipart/Form-Data; boundary=abc", want: true},
		{name: "json", contentType: "application/json", want: false},
		{name: "empty", contentType: "", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/actions/upload", nil)
			req.Header.Set("Content-Type", tc.contentType)
			if got := isMultipartUploadRequest(req); got != tc.want {
				t.Fatalf("isMultipartUploadRequest() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseMobileUploadDuration(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		legacyRaw string
		want      int
		wantErr   bool
	}{
		{name: "empty", want: 0},
		{name: "duration", raw: "95", want: 95},
		{name: "legacy duration seconds", legacyRaw: "120", want: 120},
		{name: "duration wins over legacy", raw: "90", legacyRaw: "120", want: 90},
		{name: "zero allowed", raw: "0", want: 0},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "not a number", raw: "abc", wantErr: true},
		{name: "too large", raw: "86401", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMobileUploadDuration(tc.raw, tc.legacyRaw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseMobileUploadDuration() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMobileUploadDuration() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseMobileUploadDuration() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResolveMobileUploadIdentity(t *testing.T) {
	handler := &Handler{}
	tenantID := int64(4)

	t.Run("non-admin uses JWT and ignores form", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/actions/upload", nil)
		req.Form = url.Values{"tenant_id": {"9"}, "employee_id": {"99"}}
		claims := &auth.Claims{UserID: 80, UserType: auth.UserTypeEmployee, TenantID: &tenantID}
		gotTenant, gotEmployee, err := handler.resolveMobileUploadIdentity(req, claims)
		if err != nil {
			t.Fatalf("resolveMobileUploadIdentity() error = %v", err)
		}
		if gotTenant != 4 || gotEmployee != 80 {
			t.Fatalf("identity = (%d,%d), want (4,80)", gotTenant, gotEmployee)
		}
	})

	t.Run("admin may override tenant and employee", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/actions/upload", nil)
		req.Form = url.Values{"tenant_id": {"9"}, "employee_id": {"11"}}
		claims := &auth.Claims{UserID: 1, UserType: auth.UserTypeAdmin, TenantID: &tenantID}
		gotTenant, gotEmployee, err := handler.resolveMobileUploadIdentity(req, claims)
		if err != nil {
			t.Fatalf("resolveMobileUploadIdentity() error = %v", err)
		}
		if gotTenant != 9 || gotEmployee != 11 {
			t.Fatalf("identity = (%d,%d), want (9,11)", gotTenant, gotEmployee)
		}
	})

	t.Run("admin without tenant fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/recordings/actions/upload", nil)
		claims := &auth.Claims{UserID: 1, UserType: auth.UserTypeAdmin}
		if _, _, err := handler.resolveMobileUploadIdentity(req, claims); err == nil {
			t.Fatal("resolveMobileUploadIdentity() error = nil, want error")
		}
	})
}
