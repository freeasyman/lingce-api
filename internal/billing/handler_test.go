package billing

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

const testJWTSecret = "billing-test-secret"

func TestTenantEndpointsEnforceTenantScope(t *testing.T) {
	repo := &fakeRepository{
		entitlementItems: []Entitlement{{ProductCode: "yingke", ProductName: "赢客专家", Unit: "hour", Edition: EditionStandard, QuotaType: QuotaTypeOfficial, Granted: Decimal("400"), Balance: Decimal("398.5"), PeriodEnd: "2027-11-09", Status: "active"}},
		ledgerItems:      []LedgerEntry{{ID: 1, ProductCode: "yingke", Unit: "hour", Type: LedgerTypeUsage, Amount: Decimal("-1.5"), Source: "purchase", CreatedAt: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)}},
		ledgerTotal:      1,
	}
	module := &Module{handler: NewHandler(NewService(repo))}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, "")

	tenant7 := int64(7)
	employeeToken := mustToken(t, 11, auth.UserTypeEmployee, &tenant7)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	cases := []struct {
		name         string
		target       string
		token        string
		wantStatus   int
		wantStoreHit bool
	}{
		{"employee reads own entitlements", "/api/v1/tenants/7/entitlements", employeeToken, http.StatusOK, true},
		{"employee reads own ledger", "/api/v1/tenants/7/entitlement-ledger", employeeToken, http.StatusOK, true},
		{"employee cannot read another tenant entitlements", "/api/v1/tenants/9/entitlements", employeeToken, http.StatusForbidden, false},
		{"employee cannot read another tenant ledger", "/api/v1/tenants/9/entitlement-ledger", employeeToken, http.StatusForbidden, false},
		{"admin can read any tenant entitlements", "/api/v1/tenants/9/entitlements", adminToken, http.StatusOK, true},
		{"admin can read any tenant ledger", "/api/v1/tenants/9/entitlement-ledger", adminToken, http.StatusOK, true},
		{"missing token is unauthorized", "/api/v1/tenants/7/entitlements", "", http.StatusUnauthorized, false},
		{"invalid tenant id is rejected", "/api/v1/tenants/abc/entitlements", adminToken, http.StatusBadRequest, false},
		{"zero tenant id is rejected", "/api/v1/tenants/0/entitlements", adminToken, http.StatusBadRequest, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			repo.entitlementCalls = 0
			repo.ledgerCalls = 0
			response := doRequest(t, mux, http.MethodGet, test.target, test.token, "")
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			storeHit := repo.entitlementCalls > 0 || repo.ledgerCalls > 0
			if storeHit != test.wantStoreHit {
				t.Fatalf("store hit = %v, want %v", storeHit, test.wantStoreHit)
			}
		})
	}

	// 本租户请求要落到路径里的 tenant_id，服务期为空时输出 null。
	response := doRequest(t, mux, http.MethodGet, "/api/v1/tenants/7/entitlements", employeeToken, "")
	var entitlements EntitlementListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &entitlements); err != nil {
		t.Fatalf("decode entitlements response: %v (%s)", err, response.Body.String())
	}
	if repo.entitlementTenantID != 7 {
		t.Fatalf("tenant id passed to store = %d, want 7", repo.entitlementTenantID)
	}
	if len(entitlements.Items) != 1 || entitlements.ServicePeriod != nil {
		t.Fatalf("unexpected entitlements payload: %+v", entitlements)
	}
	if !strings.Contains(response.Body.String(), `"service_period":null`) {
		t.Fatalf("service_period should be null when tenant has no subscription: %s", response.Body.String())
	}
}

func TestEntitlementsIncludeZeroFilter(t *testing.T) {
	repo := &fakeRepository{}
	module := &Module{handler: NewHandler(NewService(repo))}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, "")
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	if response := doRequest(t, mux, http.MethodGet, "/api/v1/tenants/9/entitlements?include_zero=true", adminToken, ""); response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if !repo.entitlementIncludeZero {
		t.Fatal("include_zero=true must be passed to store")
	}

	if response := doRequest(t, mux, http.MethodGet, "/api/v1/tenants/9/entitlements?include_zero=maybe", adminToken, ""); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid include_zero should be 400, got %d", response.Code)
	}
}

func TestLedgerQueryParsingAndPagination(t *testing.T) {
	repo := &fakeRepository{ledgerTotal: 3}
	module := &Module{handler: NewHandler(NewService(repo))}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, "")
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodGet,
		"/api/v1/tenants/9/entitlement-ledger?product=yingke&unit=hour&from=2026-11-01&to=2026-11-30T23:59:59Z&page=0&page_size=999",
		adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var page LedgerPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode ledger response: %v (%s)", err, response.Body.String())
	}
	if page.Total != 3 || page.Page != DefaultLedgerPage || page.PageSize != MaxLedgerPageSize {
		t.Fatalf("unexpected page envelope: %+v", page)
	}
	if repo.ledgerQuery.TenantID != 9 || repo.ledgerQuery.Product != "yingke" || repo.ledgerQuery.Unit != "hour" {
		t.Fatalf("unexpected filters: %+v", repo.ledgerQuery)
	}
	if repo.ledgerQuery.From == nil || !repo.ledgerQuery.From.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("from not parsed: %+v", repo.ledgerQuery.From)
	}
	if repo.ledgerQuery.To == nil || !repo.ledgerQuery.To.Equal(time.Date(2026, 11, 30, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("to not parsed: %+v", repo.ledgerQuery.To)
	}

	for _, target := range []string{
		"/api/v1/tenants/9/entitlement-ledger?from=yesterday",
		"/api/v1/tenants/9/entitlement-ledger?unit=minute",
		"/api/v1/tenants/9/entitlement-ledger?from=2026-12-01&to=2026-11-01",
	} {
		if response := doRequest(t, mux, http.MethodGet, target, adminToken, ""); response.Code != http.StatusBadRequest {
			t.Fatalf("%s should be 400, got %d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestOpsCatalogRequiresAdminAndReturnsVersion(t *testing.T) {
	repo := &fakeRepository{
		catalogItems: []CatalogItem{{ProductCode: "yingke", ProductName: "赢客专家", Edition: EditionStandard, PriceVersion: "2027", AnnualPrice: Decimal("13200.00"), Quotas: Quantities(`{"hour":500}`), Increments: Quantities(`{"hour":90}`), IsActive: true}},
		insertItem:   CatalogItem{ProductCode: "yingke", ProductName: "赢客专家", Edition: EditionStandard, PriceVersion: "2028"},
	}
	module := &Module{handler: NewHandler(NewService(repo))}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, "")

	tenant7 := int64(7)
	employeeToken := mustToken(t, 11, auth.UserTypeEmployee, &tenant7)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	if response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/billing/catalog", employeeToken, ""); response.Code != http.StatusForbidden {
		t.Fatalf("employee should be forbidden, got %d", response.Code)
	}
	if response := doRequest(t, mux, http.MethodPost, "/api/v1/ops/billing/catalog", employeeToken, `{}`); response.Code != http.StatusForbidden {
		t.Fatalf("employee should not create catalog versions, got %d", response.Code)
	}

	response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/billing/catalog?product_code=yingke&edition=standard", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var catalog CatalogListResponse
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v (%s)", err, response.Body.String())
	}
	if len(catalog.Items) != 1 || catalog.Items[0].PriceVersion != "2027" {
		t.Fatalf("unexpected catalog payload: %+v", catalog)
	}
	if repo.catalogFilter.ProductCode != "yingke" || repo.catalogFilter.Edition != EditionStandard {
		t.Fatalf("catalog filter not passed: %+v", repo.catalogFilter)
	}
	// 金额是 JSON 数字，可被普通 JSON 客户端直接当 number 读。
	var raw map[string][]map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode catalog raw: %v", err)
	}
	if got := string(raw["items"][0]["annual_price"]); got != "13200.00" {
		t.Fatalf("annual_price = %s, want unquoted 13200.00", got)
	}

	validBody := `{"product_code":"yingke","edition":"standard","price_version":"2028","annual_price":13800,"quotas":{"hour":520},"increment_price":1600,"increments":{"hour":90}}`
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/billing/catalog", adminToken, validBody)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", response.Code, response.Body.String())
	}
	if repo.insertInput.ProductCode != "yingke" || repo.insertInput.Edition != EditionStandard || repo.insertInput.PriceVersion != "2028" {
		t.Fatalf("unexpected insert input: %+v", repo.insertInput)
	}

	repo.insertErr = ErrCatalogVersionExists
	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/billing/catalog", adminToken, validBody)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate version should be 409, got %d body=%s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "CATALOG_VERSION_EXISTS") {
		t.Fatalf("unexpected conflict body: %s", response.Body.String())
	}

	response = doRequest(t, mux, http.MethodPost, "/api/v1/ops/billing/catalog", adminToken, `{"product_code":"yingke","edition":"enterprise","price_version":"2028"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid edition should be 400, got %d body=%s", response.Code, response.Body.String())
	}
}

func mustToken(t *testing.T, userID int64, userType auth.UserType, tenantID *int64) string {
	t.Helper()
	token, _, err := auth.GenerateToken(testJWTSecret, userID, userType, tenantID, 1, 1)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func doRequest(t *testing.T, handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
