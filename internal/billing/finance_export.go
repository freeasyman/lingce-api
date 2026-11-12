package billing

// 财务三张清单的 CSV 导出：应收 / 收款 / 开票。
//
// 约定与额度流水导出一致：UTF-8 BOM + encoding/csv，Excel 双击打开中文不乱码；
// 列名中文，文件名带导出日期；默认按运营账号的数据权限收敛，可再按 tenantId 过滤。

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/freeasyman/lingce-api/pkg/httputil"
)

const (
	FinanceExportReceivables = "receivables"
	FinanceExportPayments    = "payments"
	FinanceExportInvoices    = "invoices"

	// MaxFinanceExportRows 是单次导出的行数上限，避免误点导出把整表拉进内存。
	MaxFinanceExportRows = 100000
)

// FinanceExport 是导出结果：文件名 + 中文表头 + 已格式化的行。
type FinanceExport struct {
	Filename string
	Header   []string
	Rows     [][]string
}

// ExportFinance 导出财务清单。
func (s *InstitutionService) ExportFinance(
	ctx context.Context,
	kind string,
	from, to *time.Time,
	tenantID int64,
	adminID int64,
) (*FinanceExport, error) {
	kind = normalizeEnum(kind)
	switch kind {
	case FinanceExportReceivables, FinanceExportPayments, FinanceExportInvoices:
	default:
		return nil, invalidf("kind must be one of receivables/payments/invoices")
	}
	if tenantID > 0 {
		if err := s.requireTenantAccess(ctx, tenantID, adminID); err != nil {
			return nil, err
		}
	}
	scope, err := s.store.GetAdminScope(ctx, adminID)
	if err != nil {
		return nil, err
	}
	orgID := int64(0)
	if !scope.CanSeeAll {
		if scope.OrgID == nil || *scope.OrgID <= 0 {
			return nil, ErrAdminScopeMissing
		}
		orgID = *scope.OrgID
	}
	if from != nil && to != nil && from.After(*to) {
		return nil, invalidf("from must not be after to")
	}
	query := FinanceExportQuery{
		Kind:     kind,
		TenantID: tenantID,
		OrgID:    orgID,
		AdminID:  adminID,
	}
	if from != nil {
		value := dateOnly(*from)
		query.From = &value
	}
	if to != nil {
		// to 按「含当天」处理：SQL 用 < 次日零点。
		value := dateOnly(*to).AddDate(0, 0, 1)
		query.ToExclusive = &value
	}

	rows, err := s.store.ListFinanceExportRows(ctx, query, MaxFinanceExportRows+1)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxFinanceExportRows {
		return nil, invalidf("导出结果超过 %d 行上限，请收窄 from/to 日期范围或补充 tenantId", MaxFinanceExportRows)
	}
	return &FinanceExport{
		Filename: fmt.Sprintf("%s-%s.csv", kind, s.clock().Format("20060102")),
		Header:   financeExportHeader(kind),
		Rows:     rows,
	}, nil
}

// financeExportHeader 返回三张清单的中文表头（顺序与 store 查询列一致）。
func financeExportHeader(kind string) []string {
	switch kind {
	case FinanceExportReceivables:
		return []string{"应收单号", "机构ID", "机构名称", "订单类型", "应收金额", "已收金额", "未收金额", "订单状态", "创建时间"}
	case FinanceExportPayments:
		return []string{"收款单号", "付款方", "金额", "到账日", "收款方式", "凭证号", "已核销", "未核销", "状态", "备注", "创建时间"}
	case FinanceExportInvoices:
		return []string{"发票号", "机构ID", "机构名称", "订单号", "金额", "发票类型", "抬头", "税号", "状态", "开票日期", "备注", "创建时间"}
	default:
		return nil
	}
}

// writeFinanceCSV 写出 UTF-8 BOM + 中文表头 + 数据行。
func writeFinanceCSV(w io.Writer, export *FinanceExport) error {
	if _, err := io.WriteString(w, csvUTF8BOM); err != nil {
		return fmt.Errorf("write csv bom: %w", err)
	}
	writer := csv.NewWriter(w)
	if err := writer.Write(export.Header); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}
	for _, row := range export.Rows {
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}

// ExportBillingFinance 是 GET /ops/billing/export/{kind} 的 HTTP handler。
func (h *Handler) ExportBillingFinance(w http.ResponseWriter, r *http.Request) {
	adminID, ok := requireAdminActor(w, r)
	if !ok {
		return
	}
	kind := strings.TrimSpace(r.PathValue("kind"))
	query := r.URL.Query()
	tenantID := int64(0)
	if raw := firstQueryValue(query.Get("tenantId"), query.Get("tenant_id")); raw != "" {
		parsed, err := parseTenantIDQuery(raw)
		if err != nil {
			httputil.WriteBadRequest(w, err.Error())
			return
		}
		tenantID = parsed
	}
	from, err := parseTimeQuery("from", query.Get("from"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	to, err := parseTimeQuery("to", query.Get("to"))
	if err != nil {
		httputil.WriteBadRequest(w, err.Error())
		return
	}
	export, err := h.institutions.ExportFinance(r.Context(), kind, from, to, tenantID, adminID)
	if err != nil {
		writeInstitutionError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", export.Filename))
	if err := writeFinanceCSV(w, export); err != nil {
		// CSV 开始写之后无法再切回 JSON，写失败通常只是客户端断开，不污染响应。
		return
	}
}
