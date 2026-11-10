package billing

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/pkg/auth"
)

// ListLedgerForExport 是 fakeRepository 的导出查询：直接返回用例预置的行。
// exportFillLimit 时返回 limit 行（含 limit 本身的边界），用来验证"超过上限返回 400"。
func (f *fakeRepository) ListLedgerForExport(_ context.Context, query LedgerQuery, limit int) ([]LedgerExportRow, error) {
	f.exportCalls++
	f.exportQuery = query
	f.exportLimit = limit
	if f.exportErr != nil {
		return nil, f.exportErr
	}
	if f.exportFillLimit {
		return make([]LedgerExportRow, limit), nil
	}
	return f.exportRows, nil
}

// newExportTestMux 装配一个把时钟固定在 2026-11-10 的 handler，保证文件名可断言。
func newExportTestMux(repo *fakeRepository) http.Handler {
	service := NewService(repo)
	service.now = func() time.Time { return time.Date(2026, 11, 10, 10, 30, 0, 0, time.UTC) }
	module := &Module{handler: NewHandler(service)}
	mux := http.NewServeMux()
	module.RegisterRoutes(mux, testJWTSecret, testInternalToken)
	return mux
}

// TestExportLedgerCSVWritesBOMHeaderAndEscapesReason 覆盖 F2 的导出格式：
// 响应头、UTF-8 BOM、固定表头，以及 reason 里逗号/引号/换行的 CSV 转义。
func TestExportLedgerCSVWritesBOMHeaderAndEscapesReason(t *testing.T) {
	linkedOrderID := int64(1234)
	trickyReason := "交付扣量：录音,分析\"复核\"\n第二行"
	repo := &fakeRepository{exportRows: []LedgerExportRow{
		{
			TenantID:      9,
			ProductCode:   "yingke",
			ProductName:   "赢客专家",
			Unit:          "hour",
			Type:          LedgerTypeUsage,
			Amount:        Decimal("-12.50"),
			Source:        "purchase",
			Reason:        trickyReason,
			LinkedOrderID: &linkedOrderID,
			Operator:      "张三",
			// 东八区 16:30 导出成 UTC 08:30，时间列统一按 UTC 输出。
			CreatedAt: time.Date(2026, 11, 10, 16, 30, 0, 0, time.FixedZone("CST", 8*3600)),
		},
		{
			TenantID:    9,
			ProductCode: "kehu",
			ProductName: "客户总监",
			Unit:        "shot",
			Type:        LedgerTypeGrant,
			Amount:      Decimal("5000"),
			Source:      LedgerSourceGift,
			Reason:      "赠送",
			Operator:    "系统",
			CreatedAt:   time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC),
		},
	}}
	mux := newExportTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodGet,
		"/api/v1/ops/entitlement-ledger/export?tenant_id=9&product=yingke&unit=hour&from=2026-11-01&to=2026-11-30",
		adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}

	if got := response.Header().Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want text/csv; charset=utf-8", got)
	}
	const wantDisposition = `attachment; filename="entitlement-ledger-9-20261110.csv"`
	if got := response.Header().Get("Content-Disposition"); got != wantDisposition {
		t.Fatalf("Content-Disposition = %q, want %q", got, wantDisposition)
	}

	body := response.Body.Bytes()
	bom := []byte(csvUTF8BOM)
	if !bytes.HasPrefix(body, bom) {
		t.Fatalf("csv must start with UTF-8 BOM, got prefix %q", string(body[:min(len(body), 8)]))
	}

	records, err := csv.NewReader(bytes.NewReader(body[len(bom):])).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected header + 2 rows, got %d rows: %v", len(records), records)
	}
	wantHeader := []string{"tenant_id", "product_code", "product_name", "unit", "type", "amount",
		"source", "reason", "linked_order_id", "operator", "created_at"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("header = %v, want %v", records[0], wantHeader)
	}

	first := records[1]
	wantFirst := []string{"9", "yingke", "赢客专家", "hour", "usage", "-12.50", "purchase",
		trickyReason, "1234", "张三", "2026-11-10 08:30:00"}
	if strings.Join(first, "\x00") != strings.Join(wantFirst, "\x00") {
		t.Fatalf("first row = %q, want %q", first, wantFirst)
	}
	// 行内真实包含换行：CSV 里必须被引号包住，解析后仍是同一字段。
	if !strings.Contains(response.Body.String(), "\"交付扣量：录音,分析\"\"复核\"\"\n第二行\"") {
		t.Fatalf("reason must be quoted and escaped in raw csv: %q", response.Body.String())
	}
	// linked_order_id 为空时输出空列，而不是 <nil>。
	if second := records[2]; second[8] != "" {
		t.Fatalf("linked_order_id for nil order = %q, want empty", second[8])
	}
	if records[2][10] != "2026-11-09 00:00:00" {
		t.Fatalf("created_at = %q, want UTC formatted timestamp", records[2][10])
	}

	// 导出走的是与分页查询同一套过滤参数解析，limit 固定为上限 +1（用于判断超限）。
	if repo.exportQuery.TenantID != 9 || repo.exportQuery.Product != "yingke" || repo.exportQuery.Unit != "hour" {
		t.Fatalf("unexpected export filters: %+v", repo.exportQuery)
	}
	if repo.exportQuery.From == nil || !repo.exportQuery.From.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("from not parsed: %+v", repo.exportQuery.From)
	}
	if repo.exportQuery.To == nil || !repo.exportQuery.To.Equal(time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("to not parsed: %+v", repo.exportQuery.To)
	}
	if repo.exportLimit != MaxLedgerExportRows+1 {
		t.Fatalf("export limit = %d, want %d", repo.exportLimit, MaxLedgerExportRows+1)
	}
}

// TestExportLedgerEmptyResultStillWritesHeader 保证没有流水时导出的仍是一个可打开的 CSV（BOM + 表头）。
func TestExportLedgerEmptyResultStillWritesHeader(t *testing.T) {
	mux := newExportTestMux(&fakeRepository{})
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/entitlement-ledger/export?tenant_id=9", adminToken, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.HasPrefix(body, csvUTF8BOM) {
		t.Fatalf("csv must start with UTF-8 BOM, got %q", body)
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, csvUTF8BOM))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 1 || strings.Join(records[0], ",") != strings.Join(ledgerCSVHeader, ",") {
		t.Fatalf("expected only the header line, got %q", body)
	}
}

// TestExportLedgerRejectsMoreThanMaxRows 覆盖一次导出上限：超限返回 400 并提示收窄时间范围。
func TestExportLedgerRejectsMoreThanMaxRows(t *testing.T) {
	repo := &fakeRepository{exportFillLimit: true}
	mux := newExportTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)

	response := doRequest(t, mux, http.MethodGet,
		"/api/v1/ops/entitlement-ledger/export?tenant_id=9&from=2026-01-01&to=2026-12-31", adminToken, "")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "100000") || !strings.Contains(body, "收窄") {
		t.Fatalf("overflow message must mention the 100000 limit and narrowing the range: %s", body)
	}
	// 超限发生在写响应头之前，所以不能漏出 CSV 的 Content-Disposition。
	if got := response.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("overflow response must not be an attachment, got %q", got)
	}
}

// TestExportLedgerRequiresAdminAndValidParams 覆盖鉴权与入参校验。
func TestExportLedgerRequiresAdminAndValidParams(t *testing.T) {
	repo := &fakeRepository{}
	mux := newExportTestMux(repo)
	adminToken := mustToken(t, 1, auth.UserTypeAdmin, nil)
	tenant7 := int64(7)
	employeeToken := mustToken(t, 11, auth.UserTypeEmployee, &tenant7)

	if response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/entitlement-ledger/export?tenant_id=7", employeeToken, ""); response.Code != http.StatusForbidden {
		t.Fatalf("employee should be forbidden, got %d body=%s", response.Code, response.Body.String())
	}
	if response := doRequest(t, mux, http.MethodGet, "/api/v1/ops/entitlement-ledger/export?tenant_id=7", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token should be 401, got %d", response.Code)
	}

	cases := []struct {
		target     string
		wantInBody string
	}{
		{"/api/v1/ops/entitlement-ledger/export", "tenant_id must be a positive integer"},
		{"/api/v1/ops/entitlement-ledger/export?tenant_id=abc", "tenant_id must be a positive integer"},
		{"/api/v1/ops/entitlement-ledger/export?tenant_id=0", "tenant_id must be a positive integer"},
		{"/api/v1/ops/entitlement-ledger/export?tenant_id=9&from=yesterday", "from must be RFC3339"},
		{"/api/v1/ops/entitlement-ledger/export?tenant_id=9&unit=minute", "unit must be one of"},
		{"/api/v1/ops/entitlement-ledger/export?tenant_id=9&from=2026-12-01&to=2026-11-01", "from must not be after to"},
	}
	for _, test := range cases {
		response := doRequest(t, mux, http.MethodGet, test.target, adminToken, "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s should be 400, got %d body=%s", test.target, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), test.wantInBody) {
			t.Fatalf("%s body = %s, want it to contain %q", test.target, response.Body.String(), test.wantInBody)
		}
	}
	// 校验失败的请求不应打到存储层。
	if repo.exportCalls != 0 {
		t.Fatalf("invalid requests must not reach the store, got %d calls", repo.exportCalls)
	}
}
