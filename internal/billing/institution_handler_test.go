package billing

// 运营端计费 HTTP 错误映射与路由注册的单元测试（不依赖数据库）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteInstitutionErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"validation", &ValidationError{Message: "参数不对"}, http.StatusBadRequest, "BAD_REQUEST"},
		{"phone conflict", &ConflictError{Sentinel: ErrInstitutionPhoneConflict, Message: "该手机号已被「A」使用"},
			http.StatusConflict, "INSTITUTION_PHONE_USED"},
		{"code conflict", ErrInstitutionCodeConflict, http.StatusConflict, "INSTITUTION_CODE_USED"},
		{"not found", ErrInstitutionNotFound, http.StatusNotFound, "INSTITUTION_NOT_FOUND"},
		{"admin not found", ErrInstitutionAdminNotFound, http.StatusNotFound, "ADMIN_NOT_FOUND"},
		{"admin phone conflict", &ConflictError{Sentinel: ErrInstitutionAdminPhoneConflict, Message: "该手机号已被「B」使用"},
			http.StatusConflict, "ADMIN_PHONE_USED"},
		{"payment not found", ErrPaymentNotFound, http.StatusNotFound, "PAYMENT_NOT_FOUND"},
		{"document not found", ErrDocumentNotFound, http.StatusNotFound, "DOCUMENT_NOT_FOUND"},
		{"peer not found", ErrPeerNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"allocation conflict", ErrAllocationConflict, http.StatusConflict, "ALLOCATION_CONFLICT"},
		{"order not allocatable", ErrOrderNotAllocatable, http.StatusConflict, "ORDER_NOT_ALLOCATABLE"},
		{"period window", ErrPeriodWindowInvalid, http.StatusBadRequest, "PERIOD_WINDOW_INVALID"},
		{"bundle mismatch", ErrBundleMismatch, http.StatusBadRequest, "BUNDLE_MISMATCH"},
		{"permission", &PermissionError{Message: "仅平台运营可改"}, http.StatusForbidden, "FORBIDDEN"},
		{"no tenant permission", ErrNoPermission, http.StatusForbidden, "FORBIDDEN"},
		{"admin scope missing", ErrAdminScopeMissing, http.StatusForbidden, "ADMIN_SCOPE_MISSING"},
		{"wrapped not opened", fmt.Errorf("activate: %w", ErrInstitutionNotOpened), http.StatusConflict, "INSTITUTION_NOT_OPENED"},
		{"order not found", ErrOrderNotFound, http.StatusNotFound, "ORDER_NOT_FOUND"},
		{"internal", errors.New("boom"), http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeInstitutionError(recorder, item.err)
			if recorder.Code != item.status {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, item.status, recorder.Body.String())
			}
			var body struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error body: %v (body=%s)", err, recorder.Body.String())
			}
			if body.Code != item.code {
				t.Fatalf("code = %q, want %q", body.Code, item.code)
			}
			if body.Message == "" {
				t.Fatalf("message 不应为空")
			}
		})
	}
}

func TestInstitutionRouteRegistration(t *testing.T) {
	h := &Handler{}
	routes := h.institutionRoutes()
	if len(routes) != 53 {
		t.Fatalf("institutionRoutes 数量 = %d, want 53", len(routes))
	}
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		key := route.Method + " " + route.Path
		if _, exists := seen[key]; exists {
			t.Fatalf("重复路由: %s", key)
		}
		seen[key] = struct{}{}
		if !route.Auth {
			t.Fatalf("路由 %s 必须鉴权", key)
		}
		if len(route.AllowedUserTypes) != 1 || route.AllowedUserTypes[0] != "admin" {
			t.Fatalf("路由 %s 应只允许 admin，实际 %v", key, route.AllowedUserTypes)
		}
		if !strings.HasPrefix(route.Path, "/api/v1/ops/billing/") {
			t.Fatalf("路由前缀不对: %s", route.Path)
		}
	}
	expected := []string{
		"GET /api/v1/ops/billing/institutions",
		"POST /api/v1/ops/billing/institutions",
		"GET /api/v1/ops/billing/institutions/{id}",
		"PUT /api/v1/ops/billing/institutions/{id}",
		"GET /api/v1/ops/billing/institutions/{id}/admins",
		"POST /api/v1/ops/billing/institutions/{id}/admins",
		"PUT /api/v1/ops/billing/institutions/{id}/admins/{admin_id}",
		"POST /api/v1/ops/billing/institutions/{id}/admins/{admin_id}/reset-password",
		"POST /api/v1/ops/billing/institutions/{id}/admins/{admin_id}/set-primary",
		"POST /api/v1/ops/billing/institutions/{id}/activate-formal",
		"POST /api/v1/ops/billing/institutions/{id}/activate-trial",
		"POST /api/v1/ops/billing/institutions/{id}/migrate-trial",
		"POST /api/v1/ops/billing/institutions/{id}/increments",
		"POST /api/v1/ops/billing/institutions/{id}/gifts",
		"POST /api/v1/ops/billing/institutions/{id}/renew",
		"POST /api/v1/ops/billing/institutions/{id}/products",
		"POST /api/v1/ops/billing/institutions/{id}/extend-trial",
		"POST /api/v1/ops/billing/institutions/{id}/payments",
		"GET /api/v1/ops/billing/institutions/{id}/ownerships",
		"POST /api/v1/ops/billing/institutions/{id}/ownerships",
		"PUT /api/v1/ops/billing/institutions/{id}/ownerships/{ownership_id}",
		"POST /api/v1/ops/billing/institutions/{id}/ownerships/{ownership_id}/close",
		"POST /api/v1/ops/billing/institutions/{id}/documents",
		"GET /api/v1/ops/billing/institutions/{id}/documents",
		"GET /api/v1/ops/billing/documents/{docId}/file",
		"GET /api/v1/ops/billing/institutions/{id}/periods",
		"POST /api/v1/ops/billing/institutions/{id}/periods",
		"PUT /api/v1/ops/billing/institutions/{id}/periods/{period_id}",
		"GET /api/v1/ops/billing/pricing",
		"GET /api/v1/ops/billing/channels",
		"POST /api/v1/ops/billing/channels",
		"POST /api/v1/ops/billing/sales",
		"GET /api/v1/ops/billing/dictionaries",
		"POST /api/v1/ops/billing/dictionaries/{kind}/items",
		"PUT /api/v1/ops/billing/dictionaries/{kind}/items/{id}",
		"POST /api/v1/ops/billing/dictionaries/{kind}/items/{id}/toggle",
		"GET /api/v1/ops/billing/payments",
		"POST /api/v1/ops/billing/payments",
		"POST /api/v1/ops/billing/payments/{id}/allocations",
		"GET /api/v1/ops/billing/invoices",
		"POST /api/v1/ops/billing/invoices",
		"POST /api/v1/ops/billing/invoices/{id}/issue",
		"POST /api/v1/ops/billing/invoices/{id}/void",
		"GET /api/v1/ops/billing/institutions/{id}/invoiceable-orders",
		"GET /api/v1/ops/billing/export/{kind}",
		"POST /api/v1/ops/billing/catalog/versions",
		"GET /api/v1/ops/billing/institutions/{id}/action-logs",
		"GET /api/v1/ops/billing/institutions/{id}/allocatable-orders",
	}
	for _, key := range expected {
		if _, ok := seen[key]; !ok {
			t.Fatalf("缺少路由: %s", key)
		}
	}
}

func TestPaymentStateDerivation(t *testing.T) {
	cases := []struct {
		settled string
		amount  string
		want    string
	}{
		{"0", "20000", "unallocated"},
		{"5000", "20000", "partial"},
		{"20000", "20000", "allocated"},
		{"30000", "20000", "allocated"},
	}
	for _, item := range cases {
		if got := derivePaymentStatus(Decimal(item.settled), Decimal(item.amount)); got != item.want {
			t.Fatalf("derivePaymentStatus(%s, %s) = %s, want %s", item.settled, item.amount, got, item.want)
		}
	}
	if got := paymentState(PaymentStatusVoid, "0", "100"); got != "已作废" {
		t.Fatalf("作废状态展示不对: %s", got)
	}
	if got := paymentState(PaymentStatusPartial, "10", "100"); got != "部分核销" {
		t.Fatalf("部分核销状态展示不对: %s", got)
	}
}
