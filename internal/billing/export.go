package billing

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"time"
)

// 流水 CSV 导出的约定（F2）：
//
//   - 一次最多导出 MaxLedgerExportRows 行，超过返回 400 并要求收窄时间范围，
//     避免运营误点导出把整张流水表拉进内存、也避免前端等到超时；
//   - 文件用 UTF-8 BOM 开头 + encoding/csv 生成，Excel 直接双击打开中文不乱码，
//     reason 里的逗号/引号/换行由 encoding/csv 负责加引号转义。
const (
	// MaxLedgerExportRows 是单次导出的行数上限。
	MaxLedgerExportRows = 100000

	// ledgerCSVTimeLayout 是 created_at 的输出格式：可排序、Excel 能识别成时间，
	// 统一按 UTC 输出，避免同一份流水在不同时区导出时时间列不一致。
	ledgerCSVTimeLayout = "2006-01-02 15:04:05"

	// csvUTF8BOM 让 Excel 把文件识别成 UTF-8（没有 BOM 时中文列会乱码）。
	csvUTF8BOM = "\xef\xbb\xbf"
)

// ledgerCSVHeader 是导出 CSV 的表头，顺序即列顺序（财务/运维按列名对账）。
var ledgerCSVHeader = []string{
	"tenant_id",
	"product_code",
	"product_name",
	"unit",
	"type",
	"amount",
	"source",
	"reason",
	"linked_order_id",
	"operator",
	"created_at",
}

// LedgerExportRow 是导出的一行流水：流水字段 + 价目表里的产品名（流水表本身不存产品名）。
type LedgerExportRow struct {
	TenantID      int64
	ProductCode   string
	ProductName   string
	Unit          string
	Type          string
	Amount        Decimal
	Source        string
	Reason        string
	LinkedOrderID *int64
	Operator      string
	CreatedAt     time.Time
}

// LedgerExport 是导出结果：下载文件名 + 待写出的行。
//
// 文件名与行分开返回，handler 可以先把 Content-Disposition 写好再流式写 CSV，
// 单测也能直接断言文件名规则而不必解析 HTTP 头。
type LedgerExport struct {
	Filename string
	Rows     []LedgerExportRow
}

// ExportLedger 导出额度流水（F2）。
//
// 过滤参数校验与分页查询共用 normalizeLedgerFilters，导出只是不分页；
// 一次性取 MaxLedgerExportRows+1 行来判断是否超限（多取的那一行只用于报错）。
func (s *Service) ExportLedger(ctx context.Context, query LedgerQuery) (*LedgerExport, error) {
	query, err := normalizeLedgerFilters(query)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListLedgerForExport(ctx, query, MaxLedgerExportRows+1)
	if err != nil {
		return nil, fmt.Errorf("export ledger: %w", err)
	}
	if len(rows) > MaxLedgerExportRows {
		return nil, invalidf("导出结果超过 %d 行上限，请收窄 from/to 时间范围，或补充 product/unit 过滤后重试",
			MaxLedgerExportRows)
	}
	if rows == nil {
		rows = []LedgerExportRow{}
	}
	return &LedgerExport{
		Filename: fmt.Sprintf("entitlement-ledger-%d-%s.csv", query.TenantID, s.clock().Format("20060102")),
		Rows:     rows,
	}, nil
}

// writeLedgerCSV 把导出结果写成 UTF-8 BOM + CSV 文本。
//
// 先写 BOM 再写表头，保证 Excel 用 UTF-8 打开；字段转义交给 encoding/csv，
// 因此 reason 里出现逗号、双引号、换行都能被正确还原。
func writeLedgerCSV(w io.Writer, rows []LedgerExportRow) error {
	if _, err := io.WriteString(w, csvUTF8BOM); err != nil {
		return fmt.Errorf("write csv bom: %w", err)
	}
	writer := csv.NewWriter(w)
	if err := writer.Write(ledgerCSVHeader); err != nil {
		return fmt.Errorf("write csv header: %w", err)
	}

	record := make([]string, len(ledgerCSVHeader))
	for _, row := range rows {
		record[0] = strconv.FormatInt(row.TenantID, 10)
		record[1] = row.ProductCode
		record[2] = row.ProductName
		record[3] = row.Unit
		record[4] = row.Type
		record[5] = row.Amount.String()
		record[6] = row.Source
		record[7] = row.Reason
		record[8] = ""
		if row.LinkedOrderID != nil {
			record[8] = strconv.FormatInt(*row.LinkedOrderID, 10)
		}
		record[9] = row.Operator
		record[10] = row.CreatedAt.UTC().Format(ledgerCSVTimeLayout)
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("write csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush csv: %w", err)
	}
	return nil
}
