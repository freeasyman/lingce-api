package billing

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeCatalogVersionRequiresApproverAndReason(t *testing.T) {
	base := CreateCatalogVersionV2Request{
		ProductCode:    "yingke",
		Unit:           "hour",
		AnnualPrice:    "13000",
		IncrementPrice: "1600",
		IncrementSize:  "90",
		EffectiveFrom:  "2026-11-13",
		Approver:       "王院长",
		Reason:         "成本上涨",
	}
	cases := []struct {
		name    string
		mutate  func(*CreateCatalogVersionV2Request)
		wantSub string
	}{
		{"missing approver", func(r *CreateCatalogVersionV2Request) { r.Approver = "" }, "审批人"},
		{"missing reason", func(r *CreateCatalogVersionV2Request) { r.Reason = "" }, "原因"},
		{"missing effective", func(r *CreateCatalogVersionV2Request) { r.EffectiveFrom = "" }, "生效"},
		{"invalid effective", func(r *CreateCatalogVersionV2Request) { r.EffectiveFrom = "2026/11/13" }, "YYYY-MM-DD"},
		{"invalid unit", func(r *CreateCatalogVersionV2Request) { r.Unit = "minute" }, "单位"},
		{"zero annual", func(r *CreateCatalogVersionV2Request) { r.AnnualPrice = "0" }, "年价"},
		{"zero increment size", func(r *CreateCatalogVersionV2Request) { r.IncrementSize = "0" }, "规格"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			req := base
			test.mutate(&req)
			_, err := req.normalize()
			if err == nil {
				t.Fatal("expected validation error")
			}
			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("expected ValidationError, got %T %v", err, err)
			}
			if !strings.Contains(validation.Message, test.wantSub) {
				t.Fatalf("message %q does not contain %q", validation.Message, test.wantSub)
			}
		})
	}

	req := base
	req.Approver = " 王院长 "
	req.Reason = " 成本上涨 "
	got, err := req.normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Approver != "王院长" || got.Reason != "成本上涨" {
		t.Fatalf("trimmed fields = %+v", got)
	}
	if got.AnnualPrice.String() != "13000.00" || got.IncrementSize.String() != "90.00" {
		t.Fatalf("amounts not normalized to cents: %+v", got)
	}
}

func TestNormalizeCatalogVersionAcceptsSnakeCaseAliases(t *testing.T) {
	req := CreateCatalogVersionV2Request{
		SnakeProductCode:    "yingke",
		SnakeUnit:           "hour",
		SnakeAnnualPrice:    "13200",
		SnakeIncrementPrice: "1700",
		SnakeIncrementSize:  "100",
		SnakeEffectiveFrom:  "2026-12-01",
		Approver:            "老板",
		Reason:              "年度调价",
	}
	got, err := req.normalize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProductCode != "yingke" || got.Unit != "hour" || got.EffectiveFrom.Format("2006-01-02") != "2026-12-01" {
		t.Fatalf("alias normalization failed: %+v", got)
	}
}
