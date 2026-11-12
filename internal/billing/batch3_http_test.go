package billing_test

// 第 3 批 HTTP 层集成测试：走真实路由 + JWT + PostgreSQL，验证前端实际调用的路径与响应格式。
//   · POST/GET /ops/billing/invoices、issue、void；
//   · GET /ops/billing/export/invoices 的 CSV 头（text/csv + BOM + 中文列名）；
//   · POST /ops/billing/catalog/versions 与 GET 机构变更记录。

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
	"github.com/freeasyman/lingce-api/pkg/auth"
)

const batch3JWTSecret = "batch3-http-test-secret"

func batch3Token(t *testing.T, adminID int64) string {
	t.Helper()
	token, _, err := auth.GenerateToken(batch3JWTSecret, adminID, auth.UserTypeAdmin, nil, 1, 1)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token
}

func batch3Request(t *testing.T, handler http.Handler, method, target, token, body string) *httptest.ResponseRecorder {
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

func batch3Decode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, response.Body.String())
	}
}

func TestBatch3HTTPInvoiceExportAndCatalog(t *testing.T) {
	env := newBillingTestEnv(t)
	cleanupBatch3Logs(t, env)

	module := billing.NewModule(env.pool)
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, batch3JWTSecret, "")
	token := batch3Token(t, env.adminID)

	channelName, salesName := env.seedChannelWithSales("HTTP")
	institution := env.createInstitution("HTTP测试门诊", "口腔", []string{"口腔科"}, channelName, salesName)
	env.activateFormalUnpaid(institution.ID, "yingke")
	orderID := env.firstOrderID(institution.ID)

	// 1. 新建开票记录（真实 JSON 请求体：数字金额 + camelCase）。
	createBody := fmt.Sprintf(`{"tenantId":%d,"orderId":%d,"amount":12600,"invoiceType":"增值税专用","title":"HTTP测试门诊部有限公司","taxNo":"91310000HTTPTEST","note":"HTTP 联调"}`,
		institution.ID, orderID)
	created := batch3Request(t, mux, http.MethodPost, "/api/v1/ops/billing/invoices", token, createBody)
	if created.Code != http.StatusOK {
		t.Fatalf("创建发票 status = %d, body=%s", created.Code, created.Body.String())
	}
	var invoice billing.Invoice
	batch3Decode(t, created, &invoice)
	if invoice.ID <= 0 || invoice.Status != billing.InvoiceStatusApplied || invoice.Amount.String() != "12600.00" {
		t.Fatalf("创建发票返回不对: %+v", invoice)
	}

	// 2. 列表按 tenantId 过滤。
	listed := batch3Request(t, mux, http.MethodGet,
		fmt.Sprintf("/api/v1/ops/billing/invoices?tenantId=%d", institution.ID), token, "")
	if listed.Code != http.StatusOK {
		t.Fatalf("发票列表 status = %d, body=%s", listed.Code, listed.Body.String())
	}
	var list billing.InvoiceListResult
	batch3Decode(t, listed, &list)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].TenantName != "HTTP测试门诊" {
		t.Fatalf("发票列表返回不对: %+v", list)
	}

	// 3. 标记已开 + 作废。
	issueBody := fmt.Sprintf(`{"invoiceNo":"INV-HTTP-%s","issuedOn":%q}`, env.suffix, time.Now().Format("2006-01-02"))
	issued := batch3Request(t, mux, http.MethodPost,
		fmt.Sprintf("/api/v1/ops/billing/invoices/%d/issue", invoice.ID), token, issueBody)
	if issued.Code != http.StatusOK {
		t.Fatalf("标记已开 status = %d, body=%s", issued.Code, issued.Body.String())
	}
	var issuedInvoice billing.Invoice
	batch3Decode(t, issued, &issuedInvoice)
	if issuedInvoice.Status != billing.InvoiceStatusIssued || issuedInvoice.InvoiceNo != "INV-HTTP-"+env.suffix {
		t.Fatalf("标记已开返回不对: %+v", issuedInvoice)
	}

	// 4. 导出 CSV：Content-Type / BOM / 中文表头 / 本机构发票号。
	exported := batch3Request(t, mux, http.MethodGet,
		fmt.Sprintf("/api/v1/ops/billing/export/invoices?tenantId=%d", institution.ID), token, "")
	if exported.Code != http.StatusOK {
		t.Fatalf("导出发票 status = %d, body=%s", exported.Code, exported.Body.String())
	}
	if got := exported.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	body := exported.Body.Bytes()
	if !bytes.HasPrefix(body, []byte(csvUTF8BOM())) {
		t.Fatalf("CSV 缺少 UTF-8 BOM: %q", string(body[:min(len(body), 8)]))
	}
	records, err := csv.NewReader(bytes.NewReader(body[len(csvUTF8BOM()):])).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV 应有表头 + 1 行，实际 %d: %v", len(records), records)
	}
	if !strings.Contains(strings.Join(records[0], ","), "发票号") || !strings.Contains(strings.Join(records[0], ","), "机构名称") {
		t.Fatalf("CSV 表头不是中文列名: %v", records[0])
	}
	if records[1][0] != "INV-HTTP-"+env.suffix {
		t.Fatalf("CSV 数据行发票号不对: %v", records[1])
	}

	// 5. 作废。
	voided := batch3Request(t, mux, http.MethodPost,
		fmt.Sprintf("/api/v1/ops/billing/invoices/%d/void", invoice.ID), token, `{"note":"HTTP 作废"}`)
	if voided.Code != http.StatusOK {
		t.Fatalf("作废发票 status = %d, body=%s", voided.Code, voided.Body.String())
	}
	var voidedInvoice billing.Invoice
	batch3Decode(t, voided, &voidedInvoice)
	if voidedInvoice.Status != billing.InvoiceStatusVoided {
		t.Fatalf("作废发票返回不对: %+v", voidedInvoice)
	}

	// 6. 改价接口：审批人/原因必填、写 system_action_logs。
	const productCode = "b3http"
	_, _ = env.pool.Exec(env.ctx, `DELETE FROM product_catalog WHERE product_code = $1`, productCode)
	_, _ = env.pool.Exec(env.ctx, `DELETE FROM system_action_logs WHERE object_id = $1`, productCode)
	t.Cleanup(func() {
		_, _ = env.pool.Exec(env.ctx, `DELETE FROM product_catalog WHERE product_code = $1`, productCode)
		_, _ = env.pool.Exec(env.ctx, `DELETE FROM system_action_logs WHERE object_id = $1`, productCode)
	})
	if _, err := env.pool.Exec(env.ctx, `
INSERT INTO product_catalog (product_code, product_name, edition, price_version, annual_price, quotas, increment_price, increments)
VALUES ($1, 'HTTP 改价测试', 'standard', '2026', 1000, '{"hour":10}'::jsonb, 100, '{"hour":5}'::jsonb)`, productCode); err != nil {
		t.Fatalf("插入 HTTP 测试产品失败: %v", err)
	}
	versionBody := fmt.Sprintf(`{"productCode":%q,"unit":"hour","annualPrice":1200,"incrementPrice":150,"incrementSize":7,"effectiveFrom":%q,"approver":"王院长","reason":"HTTP 改价联调"}`,
		productCode, time.Now().Format("2006-01-02"))
	version := batch3Request(t, mux, http.MethodPost, "/api/v1/ops/billing/catalog/versions", token, versionBody)
	if version.Code != http.StatusCreated {
		t.Fatalf("新增价目版本 status = %d, body=%s", version.Code, version.Body.String())
	}
	var item billing.CatalogItem
	if err := json.Unmarshal(version.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode catalog item: %v (%s)", err, version.Body.String())
	}
	if item.AnnualPrice.String() != "1200.00" || item.EffectiveFrom == "" {
		t.Fatalf("新增价目版本返回不对: %+v", item)
	}

	// 7. 机构变更记录：真实 system_action_logs 行。
	logsResponse := batch3Request(t, mux, http.MethodGet,
		fmt.Sprintf("/api/v1/ops/billing/institutions/%d/action-logs", institution.ID), token, "")
	if logsResponse.Code != http.StatusOK {
		t.Fatalf("变更记录 status = %d, body=%s", logsResponse.Code, logsResponse.Body.String())
	}
	var logs struct {
		Items []billing.InstitutionActionLog `json:"items"`
		Total int                            `json:"total"`
	}
	batch3Decode(t, logsResponse, &logs)
	if logs.Total == 0 || len(logs.Items) == 0 {
		t.Fatalf("变更记录不应为空: %+v", logs)
	}
	seen := map[string]bool{}
	for _, log := range logs.Items {
		seen[log.ActionCode] = true
	}
	if !seen["billing.invoice.create"] || !seen["billing.invoice.issue"] || !seen["billing.invoice.void"] {
		t.Fatalf("变更记录缺少开票动作: %+v", logs.Items)
	}
}

// csvUTF8BOM 返回导出的 BOM 前缀（测试与实现常量保持一致）。
func csvUTF8BOM() string { return "\xef\xbb\xbf" }
