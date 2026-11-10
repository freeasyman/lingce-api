package billing

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testInternalToken = "billing-internal-token"

func newInternalTestMux(repo Repository) http.Handler {
	module := &Module{handler: NewHandler(NewService(repo))}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, testInternalToken)
	return mux
}

func doInternalRequest(t *testing.T, handler http.Handler, target, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(http.MethodPost, target, reader)
	if token != "" {
		request.Header.Set("X-Internal-Token", token)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeErrorPayload(t *testing.T, response *httptest.ResponseRecorder) (code string, details struct {
	Unit      string  `json:"unit"`
	Current   float64 `json:"current"`
	Required  float64 `json:"required"`
	Shortfall float64 `json:"shortfall"`
}) {
	t.Helper()
	var payload struct {
		Code    string `json:"code"`
		Details struct {
			Unit      string  `json:"unit"`
			Current   float64 `json:"current"`
			Required  float64 `json:"required"`
			Shortfall float64 `json:"shortfall"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, response.Body.String())
	}
	return payload.Code, payload.Details
}

const deductBody = `{"tenant_id":9,"product_code":"yingke",
	"items":[{"unit":"hour","amount":12.5}],
	"idempotency_key":"deal:recording:123:analysis",
	"reason":"成交分析交付"}`

func TestInternalDeductEndpointReturnsLinesAndBalances(t *testing.T) {
	repo := &fakeRepository{
		deductTenantID: 9,
		deductRows:     []Entitlement{officialRow("hour", "400", "0", "0", "400")},
		deductKeys:     map[string]struct{}{},
	}
	mux := newInternalTestMux(repo)

	response := doInternalRequest(t, mux, "/api/v1/internal/billing/deduct", testInternalToken, deductBody)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var result DeductResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode deduct result: %v (%s)", err, response.Body.String())
	}
	t.Logf("deduct response: %s", strings.TrimSpace(response.Body.String()))
	if len(result.Lines) != 1 || result.Lines[0].Unit != "hour" || result.Lines[0].Official != 12.5 || result.Lines[0].Amount != 12.5 {
		t.Fatalf("unexpected lines: %+v", result.Lines)
	}
	if result.Paused {
		t.Fatal("paused must be false when quota remains")
	}
	if len(result.Balances) != 1 {
		t.Fatalf("unexpected balances: %+v", result.Balances)
	}
	balance := result.Balances[0]
	if balance.Balance != Decimal("387.50") || balance.Used != Decimal("12.50") || balance.Status != entitlementStatusActive {
		t.Fatalf("unexpected balance: %+v", balance)
	}
	if balance.PeriodEnd != "2027-11-09" {
		t.Fatalf("period_end should be passed through: %+v", balance)
	}
	if len(repo.usageLedger) != 1 || repo.usageLedger[0].IdempotencyKey == nil ||
		*repo.usageLedger[0].IdempotencyKey != "deal:recording:123:analysis" {
		t.Fatalf("unexpected ledger: %+v", repo.usageLedger)
	}

	// 同 key 再来一次：409 ALREADY_DEDUCTED，且余额不再变化。
	response = doInternalRequest(t, mux, "/api/v1/internal/billing/deduct", testInternalToken, deductBody)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	code, _ := decodeErrorPayload(t, response)
	if code != "ALREADY_DEDUCTED" {
		t.Fatalf("duplicate code = %q, want ALREADY_DEDUCTED", code)
	}
	if repo.commitCalls != 1 || len(repo.usageLedger) != 1 || repo.deductRows[0].Balance != Decimal("387.50") {
		t.Fatalf("duplicate must not deduct again: commits=%d ledger=%d balance=%+v",
			repo.commitCalls, len(repo.usageLedger), repo.deductRows[0])
	}
}

func TestInternalDeductEndpointRejectsMissingToken(t *testing.T) {
	repo := &fakeRepository{deductTenantID: 9, deductKeys: map[string]struct{}{}}
	mux := newInternalTestMux(repo)

	if response := doInternalRequest(t, mux, "/api/v1/internal/billing/deduct", "", deductBody); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing internal token should be 401, got %d", response.Code)
	}
	if response := doInternalRequest(t, mux, "/api/v1/internal/billing/deduct", "wrong-token", deductBody); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong internal token should be 401, got %d", response.Code)
	}
	if response := doInternalRequest(t, mux, "/api/v1/internal/billing/check-quota", "", deductBody); response.Code != http.StatusUnauthorized {
		t.Fatalf("check-quota without token should be 401, got %d", response.Code)
	}
	if repo.beginDeductCalls != 0 || repo.commitCalls != 0 {
		t.Fatalf("unauthorized requests must not touch the store: %+v", repo)
	}
}

func TestInternalDeductEndpointInsufficientEntitlement(t *testing.T) {
	repo := &fakeRepository{
		deductTenantID: 9,
		deductRows:     []Entitlement{officialRow("hour", "0", "0", "0", "0")},
		deductKeys:     map[string]struct{}{},
	}
	mux := newInternalTestMux(repo)

	response := doInternalRequest(t, mux, "/api/v1/internal/billing/deduct", testInternalToken,
		`{"tenant_id":9,"product_code":"yingke","items":[{"unit":"hour","amount":3}],"idempotency_key":"deal:recording:1:analysis"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	code, details := decodeErrorPayload(t, response)
	if code != "INSUFFICIENT_ENTITLEMENT" {
		t.Fatalf("code = %q, want INSUFFICIENT_ENTITLEMENT", code)
	}
	t.Logf("insufficient response: %s", strings.TrimSpace(response.Body.String()))
	if details.Unit != "hour" || details.Current != 0 || details.Required != 3 || details.Shortfall != 3 {
		t.Fatalf("unexpected details: %+v", details)
	}
	if repo.commitCalls != 0 || len(repo.usageLedger) != 0 {
		t.Fatalf("insufficient deduct must not write: %+v", repo)
	}
}

func TestInternalDeductEndpointRejectsInvalidRequests(t *testing.T) {
	repo := &fakeRepository{deductTenantID: 9, deductKeys: map[string]struct{}{}}
	mux := newInternalTestMux(repo)

	cases := []struct {
		name string
		body string
	}{
		{"missing idempotency key", `{"tenant_id":9,"product_code":"yingke","items":[{"unit":"hour","amount":1}]}`},
		{"malformed idempotency key", `{"tenant_id":9,"product_code":"yingke","items":[{"unit":"hour","amount":1}],"idempotency_key":"deal-recording"}`},
		{"missing tenant", `{"product_code":"yingke","items":[{"unit":"hour","amount":1}],"idempotency_key":"deal:recording:1:analysis"}`},
		{"empty items", `{"tenant_id":9,"product_code":"yingke","items":[],"idempotency_key":"deal:recording:1:analysis"}`},
		{"zero amount", `{"tenant_id":9,"product_code":"yingke","items":[{"unit":"hour","amount":0}],"idempotency_key":"deal:recording:1:analysis"}`},
		{"broken json", `{not json`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			response := doInternalRequest(t, mux, "/api/v1/internal/billing/deduct", testInternalToken, test.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", response.Code, response.Body.String())
			}
			code, _ := decodeErrorPayload(t, response)
			if code != "INVALID_REQUEST" {
				t.Fatalf("code = %q, want INVALID_REQUEST", code)
			}
		})
	}
	if repo.beginDeductCalls != 0 {
		t.Fatalf("invalid requests must not start a transaction, calls=%d", repo.beginDeductCalls)
	}
}

func TestInternalCheckQuotaEndpoint(t *testing.T) {
	repo := &fakeRepository{entitlementItems: []Entitlement{officialRow("hour", "400", "0", "0", "400")}}
	mux := newInternalTestMux(repo)

	response := doInternalRequest(t, mux, "/api/v1/internal/billing/check-quota", testInternalToken, deductBody)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var result CheckQuotaResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode check-quota response: %v (%s)", err, response.Body.String())
	}
	if !result.OK {
		t.Fatalf("unexpected check-quota result: %+v", result)
	}
	if repo.beginDeductCalls != 0 || repo.commitCalls != 0 {
		t.Fatalf("check-quota must be read-only: %+v", repo)
	}

	// 不足：409 + details（current/required），不写任何数据。
	response = doInternalRequest(t, mux, "/api/v1/internal/billing/check-quota", testInternalToken,
		`{"tenant_id":9,"product_code":"yingke","items":[{"unit":"hour","amount":500}]}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("insufficient status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	code, details := decodeErrorPayload(t, response)
	if code != "INSUFFICIENT_ENTITLEMENT" {
		t.Fatalf("code = %q, want INSUFFICIENT_ENTITLEMENT", code)
	}
	if details.Unit != "hour" || details.Current != 400 || details.Required != 500 || details.Shortfall != 100 {
		t.Fatalf("unexpected details: %+v", details)
	}
	if repo.usageLedger != nil {
		t.Fatalf("check-quota must not write ledger: %+v", repo.usageLedger)
	}
}
