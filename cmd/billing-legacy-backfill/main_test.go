package main

import (
	"testing"
	"time"

	"github.com/freeasyman/lingce-api/internal/billing"
)

func TestGuardDSNRejectsProduction(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		ok   bool
	}{
		{"本地库", "postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable", true},
		{"本地库带密码", "postgresql://yiliiang:secret@localhost:5432/lingce_dev?sslmode=disable", true},
		{"生产 RDS 主机", "postgresql://freeasyman:secret@pgm-2ze5k275270fch1pmo.pg.rds.aliyuncs.com:5432/lince_medical?sslmode=disable", false},
		{"任意阿里云主机", "postgresql://u:p@other.pg.rds.aliyuncs.com:5432/lingce_dev?sslmode=disable", false},
		{"生产库名", "postgresql://u:p@127.0.0.1:5432/lingce_prod?sslmode=disable", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := guardDSN(tc.dsn)
			if tc.ok && err != nil {
				t.Fatalf("应允许 %s，实际拒绝: %v", tc.name, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("应拒绝 %s，实际允许", tc.name)
			}
		})
	}
}

func TestParseOptionsValidatesBatchAndLists(t *testing.T) {
	base := []string{"--dsn", "postgresql://yiliiang@127.0.0.1:5432/lingce_billing_test?sslmode=disable"}
	opts, err := parseOptions(append(base, "--batch", "MIG-20261112-01", "--exclude-products", "yingke, huoke", "--tenant-ids", "1,2"))
	if err != nil {
		t.Fatalf("解析参数失败: %v", err)
	}
	if opts.Batch != "MIG-20261112-01" || len(opts.Exclude) != 2 || len(opts.TenantIDs) != 2 {
		t.Fatalf("解析结果不对: %+v", opts)
	}
	if _, err := parseOptions(append(base, "--batch", "20261112-01")); err == nil {
		t.Fatal("非法批次号应该报错")
	}
	if _, err := parseOptions(append(base, "--batch", "MIG-20261112-01", "--tenant-ids", "a")); err == nil {
		t.Fatal("非法租户 id 应该报错")
	}
	if _, err := parseOptions([]string{"--batch", "MIG-20261112-01"}); err == nil {
		t.Fatal("缺少 --dsn 应该报错")
	}
}

func TestBuildPlansDefaultAllProductsAndExclusions(t *testing.T) {
	products := []catalogProduct{
		{Code: "yingke", Name: "赢客专家", Units: []catalogUnit{{Unit: "hour", Qty: billing.Decimal("400")}}},
		{Code: "kehu", Name: "客户总监", Units: []catalogUnit{{Unit: "hour", Qty: billing.Decimal("400")}, {Unit: "shot", Qty: billing.Decimal("5000")}}},
	}
	today := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	tenants := []tenantRow{
		{ID: 1, Name: "有效租户", ExpiredOn: "2027-01-31"},
		{ID: 2, Name: "过期租户", ExpiredOn: "2026-11-01"},
	}

	plans, skipped := buildPlans(tenants, products, map[string]bool{}, false, today)
	if len(plans) != 1 || plans[0].Tenant.ID != 1 {
		t.Fatalf("过期租户应默认跳过: plans=%+v skipped=%+v", plans, skipped)
	}
	if len(plans[0].Lines) != 3 {
		t.Fatalf("默认应发全部产品全部单位，实际 %d 行", len(plans[0].Lines))
	}
	if len(skipped) != 1 || skipped[0].Tenant.ID != 2 {
		t.Fatalf("应报告 1 个跳过租户: %+v", skipped)
	}

	plans, _ = buildPlans(tenants, products, map[string]bool{"kehu": true}, true, today)
	if len(plans) != 2 {
		t.Fatalf("--include-expired 应包含过期租户，实际 %d", len(plans))
	}
	for _, plan := range plans {
		if len(plan.Lines) != 1 || plan.Lines[0].ProductCode != "yingke" {
			t.Fatalf("排除 kehu 后只应发 yingke: %+v", plan.Lines)
		}
	}

	allExcluded, skipped := buildPlans(tenants, products, map[string]bool{"yingke": true, "kehu": true}, false, today)
	if len(allExcluded) != 0 || len(skipped) != 2 {
		t.Fatalf("全部取消勾选时不应生成发放计划: plans=%+v skipped=%+v", allExcluded, skipped)
	}
}

func TestComputeRevokeQtyNeverNegative(t *testing.T) {
	cases := []struct {
		granted, balance, want string
	}{
		{"400", "400", "400.00"},
		{"400", "120.50", "120.50"},
		{"400", "0", "0.00"},
		{"400", "-5", "0.00"},
		{"0", "100", "0.00"},
	}
	for _, tc := range cases {
		got := computeRevokeQty(billing.Decimal(tc.granted), billing.Decimal(tc.balance))
		if moneyText(got.String()) != tc.want {
			t.Fatalf("computeRevokeQty(%s,%s)=%s want %s", tc.granted, tc.balance, got, tc.want)
		}
	}
}

func TestNormalizeExclusionsRejectsUnknownProduct(t *testing.T) {
	products := []catalogProduct{{Code: "yingke"}}
	if _, err := normalizeExclusions([]string{"yingke"}, products); err != nil {
		t.Fatalf("已知产品不应报错: %v", err)
	}
	if _, err := normalizeExclusions([]string{"nope"}, products); err == nil {
		t.Fatal("未知产品应报错，避免拼写错误静默跳过")
	}
}
