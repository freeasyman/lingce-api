// his-charge-import 从统一 CSV 导入收费项目（按 tenant_id+code upsert）。
//
//	go run ./cmd/his-charge-import -csv ../../lingce-data/.../解析/xxx.csv -tenant-id 1
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	csvPath := flag.String("csv", "", "统一 CSV 路径（code,name,category,unit,price,insurance_category）")
	tenantID := flag.Int64("tenant-id", 1, "租户 ID")
	dbURL := flag.String("db-url", os.Getenv("LINGCE_DEV_DB_URL"), "PostgreSQL DSN")
	dryRun := flag.Bool("dry-run", false, "只统计不写入")
	excludeRaw := flag.String("exclude", "手术,麻醉,输血,床位,中草药", "不导入的分类（逗号分隔；中草药按药品管理）")
	flag.Parse()
	if *csvPath == "" || *dbURL == "" {
		fmt.Fprintln(os.Stderr, "需要 -csv 与 -db-url（或 LINGCE_DEV_DB_URL）")
		os.Exit(1)
	}

	f, err := os.Open(*csvPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开 CSV 失败:", err)
		os.Exit(1)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, "解析 CSV 失败:", err)
		os.Exit(1)
	}
	if len(records) < 2 {
		fmt.Fprintln(os.Stderr, "CSV 没有数据行")
		os.Exit(1)
	}
	header := records[0]
	col := map[string]int{}
	for i, name := range header {
		col[strings.TrimSpace(name)] = i
	}
	need := []string{"code", "name", "category", "unit", "price", "insurance_category"}
	for _, name := range need {
		if _, ok := col[name]; !ok {
			fmt.Fprintf(os.Stderr, "CSV 缺少列: %s\n", name)
			os.Exit(1)
		}
	}

	type item struct {
		code, name, category, unit, insurance string
		price                                 float64
	}
	exclude := map[string]bool{}
	for _, name := range strings.Split(*excludeRaw, ",") {
		if name = strings.TrimSpace(name); name != "" {
			exclude[name] = true
		}
	}
	items := make([]item, 0, len(records)-1)
	skipped := 0
	for _, row := range records[1:] {
		code := strings.TrimSpace(row[col["code"]])
		name := strings.TrimSpace(row[col["name"]])
		if code == "" || name == "" {
			skipped++
			continue
		}
		category := strings.TrimSpace(row[col["category"]])
		if exclude[category] {
			skipped++
			continue
		}
		price, _ := strconv.ParseFloat(strings.TrimSpace(row[col["price"]]), 64)
		items = append(items, item{
			code: code, name: name,
			category:  strings.TrimSpace(row[col["category"]]),
			unit:      strings.TrimSpace(row[col["unit"]]),
			price:     price,
			insurance: strings.TrimSpace(row[col["insurance_category"]]),
		})
	}
	fmt.Printf("租户 %d：待导入 %d 条（跳过 %d）\n", *tenantID, len(items), skipped)
	if *dryRun {
		for _, it := range items[:min(5, len(items))] {
			fmt.Printf("  %s %s %s ¥%.2f [%s]\n", it.code, it.name, it.category, it.price, it.insurance)
		}
		return
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "连接数据库失败:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "开启事务失败:", err)
		os.Exit(1)
	}
	defer tx.Rollback(ctx)
	for _, it := range items {
		if _, err := tx.Exec(ctx, `
			INSERT INTO his_charge_items (tenant_id, code, name, category, unit, price, insurance_category)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (tenant_id, code) DO UPDATE SET
				name = EXCLUDED.name, category = EXCLUDED.category, unit = EXCLUDED.unit,
				price = EXCLUDED.price, insurance_category = EXCLUDED.insurance_category, updated_at = NOW()`,
			*tenantID, it.code, it.name, it.category, it.unit, it.price, it.insurance); err != nil {
			fmt.Fprintf(os.Stderr, "导入 %s %s 失败: %v\n", it.code, it.name, err)
			os.Exit(1)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "提交失败:", err)
		os.Exit(1)
	}
	fmt.Println("导入完成")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
