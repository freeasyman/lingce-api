package billing

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeCreateInvoiceRequest(t *testing.T) {
	cases := []struct {
		name    string
		req     CreateInvoiceRequest
		wantErr bool
	}{
		{
			name: "手填金额 + 默认普通发票",
			req:  CreateInvoiceRequest{TenantID: 5003, Amount: "100.5", Title: " 某某门诊部 "},
		},
		{
			name: "关联订单可留空金额",
			req:  CreateInvoiceRequest{TenantID: 5003, OrderID: 12, Title: "某某门诊部"},
		},
		{
			name:    "手填金额为 0 拒绝",
			req:     CreateInvoiceRequest{TenantID: 5003, Amount: "0", Title: "某某门诊部"},
			wantErr: true,
		},
		{
			name:    "缺少机构拒绝",
			req:     CreateInvoiceRequest{Amount: "100", Title: "某某门诊部"},
			wantErr: true,
		},
		{
			name:    "缺少抬头拒绝",
			req:     CreateInvoiceRequest{TenantID: 5003, Amount: "100"},
			wantErr: true,
		},
		{
			name:    "未知发票类型拒绝",
			req:     CreateInvoiceRequest{TenantID: 5003, Amount: "100", Title: "抬头", InvoiceType: "电子发票"},
			wantErr: true,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.req.normalize()
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected validation error, got %+v", got)
				}
				var validation *ValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("expected ValidationError, got %T %v", err, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.TenantID != test.req.TenantID || got.Title != "某某门诊部" {
				t.Fatalf("normalize result = %+v", got)
			}
			if got.InvoiceType != InvoiceTypeNormal {
				t.Fatalf("default invoice type = %q, want %q", got.InvoiceType, InvoiceTypeNormal)
			}
		})
	}
}

func TestNormalizeInvoiceType(t *testing.T) {
	cases := map[string]string{
		"":          InvoiceTypeNormal,
		"增值税普通":    InvoiceTypeNormal,
		"普通":        InvoiceTypeNormal,
		"normal":    InvoiceTypeNormal,
		"增值税专用":    InvoiceTypeSpecial,
		"专用":        InvoiceTypeSpecial,
		"vat_special": InvoiceTypeSpecial,
	}
	for input, want := range cases {
		got, err := normalizeInvoiceType(input)
		if err != nil {
			t.Fatalf("normalizeInvoiceType(%q) error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeInvoiceType(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := normalizeInvoiceType("电子普票"); err == nil {
		t.Fatal("unsupported invoice type should be rejected")
	}
}

func TestInvoiceableOrderGuard(t *testing.T) {
	for _, status := range []string{OrderStatusVoid, OrderStatusCancelled, OrderStatusRefunded} {
		if err := invoiceableOrderGuard(Order{Status: status}); err == nil {
			t.Fatalf("order status %s must not be invoiceable", status)
		}
	}
	for _, status := range []string{OrderStatusCreated, OrderStatusPaid, OrderStatusSettled} {
		if err := invoiceableOrderGuard(Order{Status: status}); err != nil {
			t.Fatalf("order status %s should be invoiceable: %v", status, err)
		}
	}
}

func TestInvoiceNumberForIsUniqueByID(t *testing.T) {
	if got := invoiceNumberFor(7, "2026-11-13"); got != "INV20261113000007" {
		t.Fatalf("invoiceNumberFor = %q", got)
	}
}

func TestNormalizeIssueInvoiceRequest(t *testing.T) {
	req, err := IssueInvoiceRequest{IssuedOn: "2026-11-13"}.normalize()
	if err != nil || req.IssuedOn != "2026-11-13" {
		t.Fatalf("normalize issue = %+v, err=%v", req, err)
	}
	if _, err := (IssueInvoiceRequest{IssuedOn: "2026/11/13"}).normalize(); err == nil {
		t.Fatal("invalid issuedOn must be rejected")
	}
}

func TestEffectiveCatalogVersionsSkipsFutureVersion(t *testing.T) {
	today := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	items := []CatalogItem{
		{ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "20261113", EffectiveFrom: "2026-11-13", AnnualPrice: "13000"},
		{ProductCode: "yingke", Edition: EditionStandard, PriceVersion: "20261114", EffectiveFrom: "2026-11-14", AnnualPrice: "14000"},
		{ProductCode: "yingke", Edition: EditionTrial, PriceVersion: "2026", EffectiveFrom: "", AnnualPrice: "0"},
	}
	got := effectiveCatalogVersions(items, today)
	if len(got) != 2 {
		t.Fatalf("expected 2 effective rows, got %+v", got)
	}
	for _, item := range got {
		if item.Edition == EditionStandard && item.PriceVersion != "20261113" {
			t.Fatalf("future version must not be effective yet: %+v", item)
		}
	}
}
