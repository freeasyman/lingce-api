// his-material-seed 写入妇科/中医门诊常用材料建议模板（category=材料，停用、价格 0）。
// 幂等：按 tenant_id+code 去重；若同名材料已存在（即使编码不同）则跳过，避免「一次性窥器 / 一次性阴道窥器」重复。
//
//	go run ./cmd/his-material-seed -tenant-id 1 -db-url "$LINGCE_DEV_DB_URL"
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type material struct {
	code string
	name string
	unit string
}

var materials = []material{
	{"MAT0001", "一次性窥器", "个"},
	{"MAT0002", "一次性医用手套", "双"},
	{"MAT0003", "一次性垫单", "张"},
	{"MAT0004", "一次性尿杯", "个"},
	{"MAT0005", "一次性真空采血管", "支"},
	{"MAT0006", "一次性采血针", "支"},
	{"MAT0007", "一次性冲洗头", "个"},
	{"MAT0008", "灌肠包", "个"},
	{"MAT0009", "载玻片", "片"},
	{"MAT0010", "生理盐水", "瓶"},
	{"MAT0011", "10% KOH", "瓶"},
	{"MAT0012", "艾绒", "盒"},
	{"MAT0013", "艾条", "盒"},
	{"MAT0014", "雷火灸条", "盒"},
	{"MAT0015", "贴敷膏", "盒"},
	{"MAT0016", "穴位贴敷", "贴"},
	{"MAT0017", "冲洗器", "个"},
	{"MAT0018", "阴道哑铃", "个"},
	{"MAT0019", "子宫托", "个"},
}

func main() {
	tenantID := flag.Int64("tenant-id", 1, "租户 ID")
	dbURL := flag.String("db-url", os.Getenv("LINGCE_DEV_DB_URL"), "PostgreSQL DSN")
	dryRun := flag.Bool("dry-run", false, "只统计不写入")
	flag.Parse()
	if *tenantID <= 0 {
		log.Fatal("tenant-id 必须大于 0")
	}
	if *dbURL == "" {
		log.Fatal("db-url 或 LINGCE_DEV_DB_URL 必填")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer pool.Close()

	inserted, skipped := 0, 0
	for _, item := range materials {
		var existingID int64
		var existingCode string
		err := pool.QueryRow(ctx, `
			SELECT id, code FROM his_charge_items
			WHERE tenant_id = $1 AND (code = $2 OR name = $3)
			ORDER BY CASE WHEN code = $2 THEN 0 ELSE 1 END
			LIMIT 1`, *tenantID, item.code, item.name).Scan(&existingID, &existingCode)
		switch {
		case err == nil:
			skipped++
			fmt.Printf("SKIP  %s %s（已存在 id=%d code=%s）\n", item.code, item.name, existingID, existingCode)
			continue
		case !errors.Is(err, pgx.ErrNoRows):
			log.Fatalf("查询材料 %s 失败: %v", item.name, err)
		}

		if *dryRun {
			inserted++
			fmt.Printf("DRY   %s %s\n", item.code, item.name)
			continue
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO his_charge_items
				(tenant_id, code, name, category, unit, price, status, insurance_category)
			VALUES ($1, $2, $3, '材料', $4, 0, 'inactive', '')
			ON CONFLICT (tenant_id, code) DO NOTHING`,
			*tenantID, item.code, item.name, item.unit)
		if err != nil {
			// 并发或历史数据导致冲突时按已存在处理，保持幂等。
			skipped++
			fmt.Printf("SKIP  %s %s（写入冲突: %v）\n", item.code, item.name, err)
			continue
		}
		inserted++
		fmt.Printf("OK    %s %s\n", item.code, item.name)
	}

	// 汇总：材料分类下停用且 0 价的模板数量。
	var total, inactiveZero int
	_ = pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'inactive' AND price = 0)
		FROM his_charge_items WHERE tenant_id = $1 AND category = '材料'`, *tenantID).Scan(&total, &inactiveZero)
	fmt.Printf("\n完成：新增 %d，跳过 %d（同名/同编码），材料分类共 %d 项，其中停用且 0 价 %d 项\n",
		inserted, skipped, total, inactiveZero)
	if total < len(materials) {
		os.Exit(1)
	}
}
