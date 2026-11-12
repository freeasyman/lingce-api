package billing

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func TestWriteFinanceCSVWritesBOMHeaderAndRows(t *testing.T) {
	export := &FinanceExport{
		Header: financeExportHeader(FinanceExportInvoices),
		Rows: [][]string{
			{"INV20261113000001", "5003", "测试门诊部", "BL20261113001", "12600.00", "增值税专用", "测试门诊部", "91310000TEST", "已开票", "2026-11-13", "备注,带\"引号\"", "2026-11-13 10:00"},
		},
	}
	var buffer bytes.Buffer
	if err := writeFinanceCSV(&buffer, export); err != nil {
		t.Fatalf("writeFinanceCSV error: %v", err)
	}
	body := buffer.String()
	if !strings.HasPrefix(body, csvUTF8BOM) {
		t.Fatalf("csv must start with UTF-8 BOM, got %q", body[:min(len(body), 8)])
	}
	records, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, csvUTF8BOM))).ReadAll()
	if err != nil {
		t.Fatalf("parse csv: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + 1 row, got %d", len(records))
	}
	wantHeader := []string{"发票号", "机构ID", "机构名称", "订单号", "金额", "发票类型", "抬头", "税号", "状态", "开票日期", "备注", "创建时间"}
	if strings.Join(records[0], ",") != strings.Join(wantHeader, ",") {
		t.Fatalf("header = %v", records[0])
	}
	if records[1][10] != "备注,带\"引号\"" {
		t.Fatalf("csv escaping failed: %q", records[1][10])
	}
}

func TestFinanceExportHeaderForAllKinds(t *testing.T) {
	for _, kind := range []string{FinanceExportReceivables, FinanceExportPayments, FinanceExportInvoices} {
		header := financeExportHeader(kind)
		if len(header) == 0 {
			t.Fatalf("%s header must not be empty", kind)
		}
		for _, column := range header {
			if column == "" {
				t.Fatalf("%s contains empty column name", kind)
			}
		}
	}
}

func TestFinanceExportStatusLabels(t *testing.T) {
	if got := orderStatusCSVLabel(OrderStatusVoid); got != "已作废" {
		t.Fatalf("void order label = %q", got)
	}
	if got := paymentStatusCSVLabel(PaymentStatusPartial); got != "部分核销" {
		t.Fatalf("partial payment label = %q", got)
	}
	if got := invoiceStatusLabel(InvoiceStatusIssued); got != "已开票" {
		t.Fatalf("issued invoice label = %q", got)
	}
	if got := orderTypeCSVLabel(OrderTypeIncrement); got != "增量" {
		t.Fatalf("increment order label = %q", got)
	}
}
